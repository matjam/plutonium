package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matjam/plutonium/internal/diff"
	"github.com/matjam/plutonium/internal/syntax/format"
	"github.com/matjam/plutonium/internal/syntax/scanner"
)

var fmtCmd = &cobra.Command{
	Use:   "fmt [-l] [-w] [-d] [path ...]",
	Short: "Format Plutonium source files",
	Long: `fmt formats Plutonium source the way gofmt formats Go.

With no paths, or the path "-", it formats standard input, which may be a
whole file or a list of declarations or statements. A directory is walked
for .p files. Without flags, formatted source goes to standard output.

The exit status is 2 if a file could not be read, parsed or written, 1 if
-d found a file whose formatting differs, and 0 otherwise.`,
	DisableFlagsInUseLine: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		f := cmd.Flags()
		var opts fmtOptions
		opts.list, _ = f.GetBool("list")
		opts.write, _ = f.GetBool("write")
		opts.diff, _ = f.GetBool("diff")
		if code := runFmt(opts, args, os.Stdin, os.Stdout, os.Stderr); code != 0 {
			os.Exit(code)
		}
		return nil
	},
}

func init() {
	fmtCmd.Flags().BoolP("list", "l", false, "list files whose formatting differs from pluto fmt's")
	fmtCmd.Flags().BoolP("write", "w", false, "write the result to the source file instead of standard output")
	fmtCmd.Flags().BoolP("diff", "d", false, "display diffs instead of rewriting files")
	rootCmd.AddCommand(fmtCmd)
}

type fmtOptions struct {
	list, write, diff bool
}

// errFormattingDiffers marks a file that -d found unformatted.
var errFormattingDiffers = errors.New("formatting differs")

// runFmt formats the given paths and returns the exit status.
func runFmt(opts fmtOptions, paths []string, stdin io.Reader, stdout, stderr io.Writer) int {
	status := 0
	report := func(err error) {
		if err == errFormattingDiffers {
			status = max(status, 1)
			return
		}
		scanner.PrintError(stderr, err)
		status = 2
	}
	if len(paths) == 0 {
		paths = []string{"-"}
	}
	for _, path := range paths {
		if path == "-" {
			if opts.write {
				report(errors.New("cannot use -w with standard input"))
				continue
			}
			src, err := io.ReadAll(stdin)
			if err != nil {
				report(err)
				continue
			}
			if err := fmtSource(opts, "<standard input>", src, true, 0, stdout); err != nil {
				report(err)
			}
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			report(err)
			continue
		}
		if !info.IsDir() {
			if err := fmtFile(opts, path, info, stdout); err != nil {
				report(err)
			}
			continue
		}
		err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				report(err)
				return nil
			}
			if d.IsDir() || !isSourceFile(d.Name()) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				report(err)
				return nil
			}
			if err := fmtFile(opts, p, info, stdout); err != nil {
				report(err)
			}
			return nil
		})
		if err != nil {
			report(err)
		}
	}
	return status
}

func isSourceFile(name string) bool {
	return !strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".p")
}

func fmtFile(opts fmtOptions, path string, info fs.FileInfo, stdout io.Writer) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return fmtSource(opts, path, src, false, info.Mode().Perm(), stdout)
}

// fmtSource formats one file's source and acts on the result as gofmt
// does: list, write, diff, or print.
func fmtSource(opts fmtOptions, name string, src []byte, fragmentOk bool, perm fs.FileMode, stdout io.Writer) error {
	res, err := format.File(name, src, fragmentOk)
	if err != nil {
		return err
	}
	if !bytes.Equal(src, res) {
		if opts.list {
			if _, err := fmt.Fprintln(stdout, name); err != nil {
				return err
			}
		}
		if opts.write {
			if err := writeFormatted(name, res, perm); err != nil {
				return err
			}
		}
		if opts.diff {
			newName := filepath.ToSlash(name)
			if _, err := stdout.Write(diff.Diff(newName+".orig", src, newName, res)); err != nil {
				return err
			}
			return errFormattingDiffers
		}
	}
	if !opts.list && !opts.write && !opts.diff {
		_, err = stdout.Write(res)
	}
	return err
}

// writeFormatted replaces a file's contents through a temporary file in
// the same directory, so a failed write leaves the original intact.
func writeFormatted(name string, data []byte, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(name), filepath.Base(name)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmpName, perm)
	}
	if werr == nil {
		werr = os.Rename(tmpName, name)
	}
	if werr != nil {
		_ = os.Remove(tmpName)
	}
	return werr
}
