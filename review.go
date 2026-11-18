package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// reviewDirName is the subfolder of the scanned directory that all suspected
// duplicates are moved into, next to symlinks to their origins.
const reviewDirName = "suspected-duplicates"

// originSep joins the origin's name and a moved file's name, so a group's
// files sort right after the origin's symlink: "IMG_1.png", "IMG_1.png__IMG_1.jpg".
const originSep = "__"

// reviewGroup is a similar group with its planned destinations. files[0] is
// the origin: it stays in place and is linked from link. files[i+1] moves to
// dests[i].
type reviewGroup struct {
	similarGroup
	link  string // symlink to create; "" if a correct one already exists
	dests []string
}

// planReview decides every destination path before anything is touched, so
// the printed plan matches what happens. Moved files are renamed to
// "<origin>__<name>"; if that is already taken in the review folder (e.g. by
// an earlier, unreviewed run) it gets a "-2", "-3", ... suffix.
func planReview(root string, groups []similarGroup) []reviewGroup {
	reviewRoot := filepath.Join(root, reviewDirName)
	used := map[string]bool{}
	var plan []reviewGroup
	for _, g := range groups {
		origin := g.files[0].name
		rg := reviewGroup{similarGroup: g}
		if !isLinkTo(filepath.Join(reviewRoot, origin), originTarget(origin)) {
			rg.link = filepath.Join(reviewRoot, uniqueName(reviewRoot, origin, used))
		} else {
			used[origin] = true
		}
		for _, f := range g.files[1:] {
			name := uniqueName(reviewRoot, origin+originSep+f.name, used)
			rg.dests = append(rg.dests, filepath.Join(reviewRoot, name))
		}
		plan = append(plan, rg)
	}
	return plan
}

// originTarget is the relative symlink target for an origin, seen from
// inside the review folder.
func originTarget(origin string) string {
	return filepath.Join("..", origin)
}

func isLinkTo(path, target string) bool {
	got, err := os.Readlink(path)
	return err == nil && got == target
}

// uniqueName returns name, or name with a "-N" suffix before the extension,
// such that it is neither on disk in dir nor already in used. It marks the
// returned name as used.
func uniqueName(dir, name string, used map[string]bool) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	candidate := name
	for n := 2; ; n++ {
		if _, err := os.Lstat(filepath.Join(dir, candidate)); os.IsNotExist(err) && !used[candidate] {
			used[candidate] = true
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d%s", base, n, ext)
	}
}

// moveToReview links each origin into the review folder and moves the other
// members there. It never overwrites an existing file.
func moveToReview(root string, plan []reviewGroup, warn func(error)) (moved, failed int) {
	if err := os.MkdirAll(filepath.Join(root, reviewDirName), 0o755); err != nil {
		warn(err)
		for _, g := range plan {
			failed += len(g.dests)
		}
		return 0, failed
	}
	for _, g := range plan {
		if g.link != "" {
			if err := os.Symlink(originTarget(g.files[0].name), g.link); err != nil {
				warn(fmt.Errorf("could not link origin %s: %w", g.files[0].name, err))
			}
		}
		for i, f := range g.files[1:] {
			dst := g.dests[i]
			if _, err := os.Lstat(dst); !os.IsNotExist(err) {
				warn(fmt.Errorf("not moving %s: %s already exists", f.path, dst))
				failed++
				continue
			}
			if err := os.Rename(f.path, dst); err != nil {
				warn(err)
				failed++
				continue
			}
			moved++
		}
	}
	return moved, failed
}
