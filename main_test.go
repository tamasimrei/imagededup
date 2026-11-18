package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// blockImage draws a random pattern of 16x16 coloured blocks; different
// seeds give visually unrelated images.
func blockImage(seed int64, w, h int) image.Image {
	rng := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for by := 0; by < h; by += 16 {
		for bx := 0; bx < w; bx += 16 {
			c := color.RGBA{uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256)), 255}
			for y := by; y < min(by+16, h); y++ {
				for x := bx; x < min(bx+16, w); x++ {
					img.Set(x, y, c)
				}
			}
		}
	}
	return img
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, buf.Bytes())
}

func writeJPEG(t *testing.T, path string, img image.Image) {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 75}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, buf.Bytes())
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func setAge(t *testing.T, path string, age time.Duration) {
	t.Helper()
	ts := time.Now().Add(-age)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// fixture creates:
//
//	original.png  - oldest copy, must be kept
//	copy.png      - identical to original.png, newer → deleted
//	reencoded.jpg - original re-encoded as JPEG → suspected duplicate
//	other.png     - unrelated image
func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	img := blockImage(1, 320, 240)
	writePNG(t, filepath.Join(dir, "original.png"), img)
	writePNG(t, filepath.Join(dir, "copy.png"), img)
	writeJPEG(t, filepath.Join(dir, "reencoded.jpg"), img)
	writePNG(t, filepath.Join(dir, "other.png"), blockImage(2, 320, 240))
	writeFile(t, filepath.Join(dir, "notes.txt"), []byte("not an image"))
	setAge(t, filepath.Join(dir, "original.png"), 2*time.Hour)
	setAge(t, filepath.Join(dir, "copy.png"), time.Hour)
	return dir
}

func runCLI(t *testing.T, stdin string, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String() + errOut.String()
}

func TestYesDeletesWithoutPrompt(t *testing.T) {
	dir := fixture(t)
	code, out := runCLI(t, "", "-y", dir)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if exists(filepath.Join(dir, "copy.png")) {
		t.Error("copy.png should have been deleted")
	}
	for _, keep := range []string{"original.png", "other.png", "notes.txt"} {
		if !exists(filepath.Join(dir, keep)) {
			t.Errorf("%s should still be in place", keep)
		}
	}
	if strings.Contains(out, "[y/N]") {
		t.Error("-y should not prompt")
	}
	assertReviewed(t, dir, "original.png", "original.png__reencoded.jpg")
	if exists(filepath.Join(dir, "reencoded.jpg")) {
		t.Error("reencoded.jpg should have been moved")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, reviewDirName))
	if len(entries) != 2 {
		t.Errorf("want the moved file and one symlink in %s, got %d entries", reviewDirName, len(entries))
	}
}

// assertReviewed checks that <dir>/suspected-duplicates holds the moved files
// (as regular files, no subfolders) plus a symlink that resolves to the
// origin in dir.
func assertReviewed(t *testing.T, dir, origin string, moved ...string) {
	t.Helper()
	rdir := filepath.Join(dir, reviewDirName)
	for _, m := range moved {
		if info, err := os.Lstat(filepath.Join(rdir, m)); err != nil || !info.Mode().IsRegular() {
			t.Errorf("%s should have been moved into %s", m, rdir)
		}
	}
	link := filepath.Join(rdir, origin)
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s should be a symlink", link)
	}
	got, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(dir, origin))
	if got != want {
		t.Errorf("symlink resolves to %s, want %s", got, want)
	}
}

func TestOriginIsOldestAndNamesDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	img := blockImage(3, 320, 240)
	writeJPEG(t, filepath.Join(dir, "b.jpg"), img)
	writePNG(t, filepath.Join(dir, "a.png"), img)
	setAge(t, filepath.Join(dir, "b.jpg"), time.Hour) // older, so b.jpg is the origin

	// Leftovers from an earlier, unreviewed run: a file with the same name as
	// the one about to be moved, and a correct symlink to the origin.
	rdir := filepath.Join(dir, reviewDirName)
	if err := os.MkdirAll(rdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(rdir, "b.jpg__a.png"), []byte("earlier run"))
	if err := os.Symlink(filepath.Join("..", "b.jpg"), filepath.Join(rdir, "b.jpg")); err != nil {
		t.Fatal(err)
	}

	code, out := runCLI(t, "", "-y", dir)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	assertReviewed(t, dir, "b.jpg", "b.jpg__a-2.png")
	if data, _ := os.ReadFile(filepath.Join(rdir, "b.jpg__a.png")); string(data) != "earlier run" {
		t.Error("existing file in the review folder was overwritten")
	}
	if exists(filepath.Join(rdir, "b-2.jpg")) {
		t.Error("an existing correct symlink should be reused, not duplicated")
	}
	if !strings.Contains(out, "a.png -> b.jpg__a-2.png") {
		t.Errorf("plan should show the renamed destination:\n%s", out)
	}
	if !exists(filepath.Join(dir, "b.jpg")) || exists(filepath.Join(dir, "a.png")) {
		t.Error("b.jpg should stay in place and a.png should be moved")
	}
}

func TestThresholdDisabledMovesNothing(t *testing.T) {
	dir := fixture(t)
	if code, out := runCLI(t, "", "-y", "-threshold", "-1", dir); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !exists(filepath.Join(dir, "reencoded.jpg")) || exists(filepath.Join(dir, reviewDirName)) {
		t.Error("-threshold -1 must not move anything")
	}
}

func TestFlagAfterDir(t *testing.T) {
	dir := fixture(t)
	if code, out := runCLI(t, "", dir, "-y"); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if exists(filepath.Join(dir, "copy.png")) {
		t.Error("copy.png should have been deleted")
	}
}

func TestPromptDeclined(t *testing.T) {
	for _, answer := range []string{"n\n", "\n", ""} {
		dir := fixture(t)
		code, out := runCLI(t, answer, dir)
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, out)
		}
		if !strings.Contains(out, "[y/N]") {
			t.Errorf("expected a confirmation prompt:\n%s", out)
		}
		if !exists(filepath.Join(dir, "copy.png")) {
			t.Errorf("answer %q: copy.png must not be deleted", answer)
		}
		if !exists(filepath.Join(dir, "reencoded.jpg")) || exists(filepath.Join(dir, reviewDirName)) {
			t.Errorf("answer %q: nothing must be moved", answer)
		}
	}
}

func TestPromptAccepted(t *testing.T) {
	dir := fixture(t)
	if code, out := runCLI(t, "yes\n", dir); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if exists(filepath.Join(dir, "copy.png")) {
		t.Error("copy.png should have been deleted")
	}
	if !exists(filepath.Join(dir, "original.png")) {
		t.Error("original.png must be kept")
	}
	assertReviewed(t, dir, "original.png", "original.png__reencoded.jpg")
}

func TestNoDuplicates(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, filepath.Join(dir, "a.png"), blockImage(1, 64, 64))
	writePNG(t, filepath.Join(dir, "b.png"), blockImage(2, 64, 64))
	code, out := runCLI(t, "", dir)
	if code != 0 || !strings.Contains(out, "nothing to do") || strings.Contains(out, "[y/N]") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestBadArgs(t *testing.T) {
	if code, _ := runCLI(t, "", "a", "b"); code != 2 {
		t.Errorf("two dirs: exit %d, want 2", code)
	}
	if code, _ := runCLI(t, "", "-threshold", "65", "."); code != 2 {
		t.Errorf("threshold 65: exit %d, want 2", code)
	}
	if code, _ := runCLI(t, "", t.TempDir()+"/missing"); code != 1 {
		t.Errorf("missing dir: exit %d, want 1", code)
	}
}

func TestDHash(t *testing.T) {
	a := blockImage(1, 640, 480)
	resized := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := range 240 {
		for x := range 320 {
			resized.Set(x, y, a.At(x*2, y*2))
		}
	}
	if d := hammingDistance(dHash(a), dHash(resized)); d > 4 {
		t.Errorf("resized copy distance %d, want <= 4", d)
	}
	if d := hammingDistance(dHash(a), dHash(blockImage(2, 640, 480))); d <= 10 {
		t.Errorf("unrelated image distance %d, want > 10", d)
	}
}
