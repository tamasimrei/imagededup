// Command imagededup deletes byte-identical image files in a folder and lists
// visually similar images as suspected duplicates.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const usageText = `Usage: imagededup [-y] [-threshold N] [dir]

Finds duplicate images in dir (default: the current directory).
Byte-identical files are deleted, keeping one copy of each (the oldest).
Visually similar images are suspected duplicates: the oldest of each group
(the origin) stays in place, the others are moved to dir/suspected-duplicates/
and renamed to <origin>__<name>, next to a symlink to their origin, so each
group sorts together. They are never deleted.

Options:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("imagededup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	yes := fs.Bool("y", false, "delete identical files and move suspected duplicates without asking for confirmation")
	threshold := fs.Int("threshold", 10, "max perceptual-hash distance (0-64) for suspected duplicates; -1 disables the check")
	fs.Usage = func() {
		fmt.Fprint(stderr, usageText)
		fs.PrintDefaults()
	}

	positional, err := parseInterleaved(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(positional) > 1 {
		fmt.Fprintln(stderr, "imagededup: too many arguments")
		fs.Usage()
		return 2
	}
	if *threshold > 64 {
		fmt.Fprintln(stderr, "imagededup: -threshold must be at most 64")
		return 2
	}
	dir := "."
	if len(positional) == 1 {
		dir = positional[0]
	}

	warn := func(err error) { fmt.Fprintln(stderr, "warning:", err) }

	files, err := scanDir(dir)
	if err != nil {
		fmt.Fprintln(stderr, "imagededup:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Scanned %d image files in %s\n", len(files), dir)

	identical := findIdentical(files, warn)
	toDelete := 0
	var bytes int64
	if len(identical) > 0 {
		fmt.Fprintln(stdout, "\nIdentical files:")
		for _, g := range identical {
			fmt.Fprintf(stdout, "  keep    %s\n", g.keep.name)
			for _, d := range g.dups {
				fmt.Fprintf(stdout, "  delete  %s\n", d.name)
				toDelete++
				bytes += d.size
			}
		}
	}

	var review []reviewGroup
	toMove := 0
	if *threshold >= 0 {
		review = planReview(dir, findSimilar(survivors(files, identical), *threshold, warn))
		if len(review) > 0 {
			fmt.Fprintf(stdout, "\nSuspected duplicates (moved to %s/ for review):\n", reviewDirName)
			for _, g := range review {
				fmt.Fprintf(stdout, "  origin  %s  (stays, symlinked)\n", g.files[0].name)
				for j := 1; j < len(g.files); j++ {
					fmt.Fprintf(stdout, "    move  %s -> %s  (distance %d)\n",
						g.files[j].name, filepath.Base(g.dests[j-1]), g.dist[j])
					toMove++
				}
			}
		}
	}

	if toDelete == 0 && toMove == 0 {
		fmt.Fprintln(stdout, "\nNo duplicates found, nothing to do.")
		return 0
	}

	if !*yes {
		var actions []string
		if toDelete > 0 {
			actions = append(actions, fmt.Sprintf("delete %d identical file(s) (%s)", toDelete, humanBytes(bytes)))
		}
		if toMove > 0 {
			actions = append(actions, fmt.Sprintf("move %d suspected duplicate(s) to %s/", toMove, reviewDirName))
		}
		prompt := strings.Join(actions, " and ")
		fmt.Fprintf(stdout, "\n%s%s? [y/N]: ", strings.ToUpper(prompt[:1]), prompt[1:])
		answer, _ := bufio.NewReader(stdin).ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(stdout, "Aborted, nothing changed.")
			return 0
		}
	}

	code := 0
	fmt.Fprintln(stdout)
	if toDelete > 0 {
		deleted, failed := deleteDuplicates(identical, warn)
		fmt.Fprintf(stdout, "Deleted %d file(s).\n", deleted)
		if failed > 0 {
			fmt.Fprintf(stderr, "imagededup: %d file(s) could not be deleted\n", failed)
			code = 1
		}
	}
	if toMove > 0 {
		moved, failed := moveToReview(dir, review, warn)
		fmt.Fprintf(stdout, "Moved %d file(s) to %s.\n", moved, filepath.Join(dir, reviewDirName))
		if failed > 0 {
			fmt.Fprintf(stderr, "imagededup: %d file(s) could not be moved\n", failed)
			code = 1
		}
	}
	return code
}

// parseInterleaved parses flags that appear before or after positional
// arguments, so both "imagededup -y dir" and "imagededup dir -y" work.
// Everything after a "--" is treated as positional.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		consumed := len(args) - fs.NArg()
		if consumed > 0 && args[consumed-1] == "--" {
			return append(positional, fs.Args()...), nil
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
