# imagededup

A command-line tool that cleans up duplicate images in a folder.

- **Identical files** (byte-for-byte the same) are deleted. One copy is kept: the oldest.
- **Suspected duplicates** (images that look alike, such as re-saved, resized or lightly edited copies) are never deleted. They are moved into a `suspected-duplicates/` folder so you can review them.

Nothing changes until you confirm, unless you pass `-y`.

## Install

Requires Go 1.27 or later. There are no third-party dependencies.

```sh
git clone <repo-url> imagededup
cd imagededup
go build -o imagededup .
```

To install it into `$GOPATH/bin` instead, run `go install .`.

## Usage

```sh
imagededup [-y] [-threshold N] [dir]
```

| Option | Default | Meaning |
|---|---|---|
| `dir` | current directory | Folder to scan. Only its top level is scanned, not subfolders. |
| `-y` | off | Don't ask for confirmation. |
| `-threshold N` | `10` | How alike two images must be to count as suspected duplicates, as the maximum number of differing bits (0–64) between their fingerprints. Lower is stricter. `-1` turns the check off. |

Options can go before or after the folder: `imagededup ~/Pictures -y` works.

### Example

```
$ imagededup ~/Pictures/trip
Scanned 5 image files in /Users/me/Pictures/trip

Identical files:
  keep    IMG_001.png
  delete  IMG_001 (1).png
  delete  IMG_001 copy.png

Suspected duplicates (moved to suspected-duplicates/ for review):
  origin  IMG_001.png  (stays, symlinked)
    move  IMG_001.jpg -> IMG_001.png__IMG_001.jpg  (distance 0)

Delete 2 identical file(s) (6.2 KiB) and move 1 suspected duplicate(s) to suspected-duplicates/? [y/N]: y

Deleted 2 file(s).
Moved 1 file(s) to /Users/me/Pictures/trip/suspected-duplicates.
```

Any answer other than `y` or `yes` cancels the run and changes nothing.

## How it works

### Identical files

Files are first grouped by size, and only files with the same size are compared, using a SHA-256 checksum of their contents. In each set of identical files, the tool keeps the oldest by modification time. If two are equally old, it keeps the one with the shorter name, then the one that comes first alphabetically. So `IMG_001.png` is kept over `IMG_001 (1).png`. Just before deleting, the tool checks that the file it is keeping still exists.

### Suspected duplicates

Each image is shrunk to a 9×8 grayscale grid and turned into a 64-bit fingerprint (a "difference hash", or dHash). Images whose fingerprints differ in at most `-threshold` bits are grouped together. Matches chain: if A matches B and B matches C, all three end up in one group.

The **origin** of each group is chosen the same way as the kept file among identical copies (oldest, then shortest name, then alphabetical). It stays where it is. The other images in the group move into one flat review folder:

```
trip/
  IMG_001.png                         ← origin, untouched
  suspected-duplicates/
    IMG_001.png -> ../IMG_001.png     ← symlink to the origin
    IMG_001.png__IMG_001.jpg          ← moved, renamed <origin>__<name>
```

Because each moved file starts with its origin's name, a group sorts together right after its origin's symlink, so the versions are easy to compare side by side. Existing files are never overwritten. If a name is already taken, for example by an earlier run you haven't reviewed yet, the new file gets a `-2`, `-3` and so on before the extension.

**Reviewing:** delete the versions you don't want. To keep one, move it back up a level and remove everything up to and including the `__` from its name.

## Supported formats

| | JPEG, PNG, GIF | BMP, WebP, TIFF, HEIC/HEIF, AVIF |
|---|---|---|
| Identical files | ✓ | ✓ |
| Suspected duplicates | ✓ | — (not decoded, skipped without a message) |

Hidden files (names starting with `.`) and symlinks are ignored.

## Limitations

- Subfolders are not scanned.
- Rotated or mirrored copies are not detected as similar.
- Similar images are compared pair by pair, which is fine for folders of a few thousand images but slows down for very large folders.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success, including when you decline the confirmation |
| 1 | The folder could not be read, or a file could not be deleted or moved |
| 2 | Invalid arguments |

## Development

```sh
go test -race ./...                 # run all tests
go test -run TestPromptDeclined ./...  # run one test
gofmt -l . && go vet ./...          # lint
```

## License

[MIT-0](LICENSE)
