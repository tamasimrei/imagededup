# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

- Build: `go build -o imagededup .`
- Test: `go test -race ./...`
- Single test: `go test -run TestPromptDeclined ./...`
- Lint: `gofmt -l . && go vet ./...`

## Architecture

A single `main` package with no third-party dependencies (stdlib only, on purpose).

- `main.go`: CLI. `run(args, stdin, stdout, stderr) int` holds all the logic, so tests drive it without starting a process. `parseInterleaved` lets flags come after the directory argument (`imagededup dir -y`); the stdlib `flag` package alone stops at the first positional argument.
- `dedup.go`: the pipeline:
  1. `scanDir`: top-level regular files with an image extension. Not recursive; skips dotfiles and symlinks.
  2. `findIdentical`: groups by size, then SHA-256. The oldest file is kept (ties: shorter name, then alphabetical).
  3. `findSimilar`: runs on the files that survive deletion. Decodes in parallel, computes a dHash, and groups by Hamming distance with transitive union-find. Each group is sorted with `preferKeep`, so `files[0]` is the origin. Formats the stdlib can't decode (`image.ErrFormat`, e.g. WebP/HEIC) are skipped silently. They still take part in exact matching.
  4. `deleteDuplicates`: re-checks that the kept file still exists before removing its copies.
- `review.go`: suspected duplicates all go into one flat `suspected-duplicates/` folder, with no per-group subfolders. `planReview` works out every destination path before the prompt, so the printed plan matches what happens. Moved files are renamed to `<origin>__<name>` (`originSep`), so each group sorts right after its origin's symlink. Names already taken in the folder get a `-N` suffix, and an existing correct origin symlink is reused. `moveToReview` adds a relative symlink `../<origin>` for each origin and renames the other members into the folder.
- `phash.go`: 64-bit difference hash, with fast paths for `*image.YCbCr` (JPEG) and `*image.Gray`.

Invariants:
- Only byte-identical files are ever deleted.
- Similar images are only moved, never overwritten. The origin stays in place.
- A single confirmation (or `-y`) covers both deleting and moving.
- `scanDir` is non-recursive, which is what keeps the review folder from being rescanned. If you add recursion, exclude `suspected-duplicates`.

Exit codes: 0 on success (including when the user declines), 1 on runtime or deletion failure, 2 on usage errors.
