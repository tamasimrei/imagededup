package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// imageExts lists the extensions considered images. Exact-duplicate detection
// works for all of them; similarity detection only for formats registered with
// the image package (JPEG, PNG, GIF).
var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true,
	".webp": true, ".tif": true, ".tiff": true, ".heic": true, ".heif": true,
	".avif": true,
}

type imageFile struct {
	path    string
	name    string
	size    int64
	modTime time.Time
}

// scanDir returns the regular image files directly inside dir (not
// recursive). Hidden files and symlinks are skipped.
func scanDir(dir string) ([]imageFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []imageFile
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || strings.HasPrefix(name, ".") {
			continue
		}
		if !imageExts[strings.ToLower(filepath.Ext(name))] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed since ReadDir
		}
		files = append(files, imageFile{
			path:    filepath.Join(dir, name),
			name:    name,
			size:    info.Size(),
			modTime: info.ModTime(),
		})
	}
	return files, nil
}

type identicalGroup struct {
	keep imageFile
	dups []imageFile
}

// findIdentical groups byte-identical files. Only files sharing a size are
// hashed. In each group the oldest file is kept; ties go to the shorter name,
// then to alphabetical order.
func findIdentical(files []imageFile, warn func(error)) []identicalGroup {
	bySize := map[int64][]imageFile{}
	for _, f := range files {
		bySize[f.size] = append(bySize[f.size], f)
	}

	var groups []identicalGroup
	for _, same := range bySize {
		if len(same) < 2 {
			continue
		}
		byHash := map[[sha256.Size]byte][]imageFile{}
		for _, f := range same {
			sum, err := hashFile(f.path)
			if err != nil {
				warn(err)
				continue
			}
			byHash[sum] = append(byHash[sum], f)
		}
		for _, g := range byHash {
			if len(g) < 2 {
				continue
			}
			sort.Slice(g, func(i, j int) bool { return preferKeep(g[i], g[j]) })
			groups = append(groups, identicalGroup{keep: g[0], dups: g[1:]})
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].keep.name < groups[j].keep.name })
	return groups
}

func preferKeep(a, b imageFile) bool {
	if !a.modTime.Equal(b.modTime) {
		return a.modTime.Before(b.modTime)
	}
	if len(a.name) != len(b.name) {
		return len(a.name) < len(b.name)
	}
	return a.name < b.name
}

func hashFile(path string) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	f, err := os.Open(path)
	if err != nil {
		return sum, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return sum, fmt.Errorf("%s: %w", path, err)
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// survivors returns the files that would remain after deleting identical
// duplicates.
func survivors(files []imageFile, groups []identicalGroup) []imageFile {
	doomed := map[string]bool{}
	for _, g := range groups {
		for _, d := range g.dups {
			doomed[d.path] = true
		}
	}
	var out []imageFile
	for _, f := range files {
		if !doomed[f.path] {
			out = append(out, f)
		}
	}
	return out
}

type similarGroup struct {
	files []imageFile // files[0] is the origin, chosen like the kept identical file
	dist  []int       // perceptual-hash distance from files[0]
}

// findSimilar groups images whose perceptual hashes differ by at most
// threshold bits. Grouping is transitive, so a member's distance from the
// origin can exceed threshold.
func findSimilar(files []imageFile, threshold int, warn func(error)) []similarGroup {
	hashes := make([]uint64, len(files))
	ok := make([]bool, len(files))
	errs := make([]error, len(files))

	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for i := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			h, err := imageHash(files[i].path)
			switch {
			case err == nil:
				hashes[i], ok[i] = h, true
			case !errors.Is(err, image.ErrFormat):
				errs[i] = fmt.Errorf("%s: %w", files[i].path, err)
			}
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			warn(err)
		}
	}

	parent := make([]int, len(files))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	for i := range files {
		for j := i + 1; j < len(files); j++ {
			if ok[i] && ok[j] && hammingDistance(hashes[i], hashes[j]) <= threshold {
				parent[find(i)] = find(j)
			}
		}
	}

	members := map[int][]int{}
	for i := range files {
		if ok[i] {
			members[find(i)] = append(members[find(i)], i)
		}
	}
	var groups []similarGroup
	for _, idx := range members {
		if len(idx) < 2 {
			continue
		}
		sort.Slice(idx, func(a, b int) bool { return preferKeep(files[idx[a]], files[idx[b]]) })
		var g similarGroup
		for _, i := range idx {
			g.files = append(g.files, files[i])
			g.dist = append(g.dist, hammingDistance(hashes[idx[0]], hashes[i]))
		}
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].files[0].name < groups[j].files[0].name })
	return groups
}

func imageHash(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return 0, err
	}
	return dHash(img), nil
}

// deleteDuplicates removes the duplicate files of every group. A group is
// skipped if its kept file no longer exists, so a copy is never lost.
func deleteDuplicates(groups []identicalGroup, warn func(error)) (deleted, failed int) {
	for _, g := range groups {
		if _, err := os.Stat(g.keep.path); err != nil {
			warn(fmt.Errorf("kept file %s is missing, not deleting its duplicates", g.keep.path))
			failed += len(g.dups)
			continue
		}
		for _, d := range g.dups {
			if err := os.Remove(d.path); err != nil {
				warn(err)
				failed++
				continue
			}
			deleted++
		}
	}
	return deleted, failed
}
