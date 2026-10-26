package main

// pluto vet -unsafe, as cmd/pluto/vet.go has it.

import (
	"pluto/io"
	"pluto/path/filepath"
	"pluto/strings"
	"selfhost/compat"
	"selfhost/compat/fs"
	"selfhost/internal/audit"
	"selfhost/internal/diag"
	"selfhost/internal/load"
	"selfhost/internal/types2"
	"selfhost/pu"
)

const vetHelp = `vet -unsafe audits each package and its dependencies without running or
linking them. Packages may be directories, standard library paths or /... patterns.
With no packages it audits the current directory. Unsafe labels identify direct
operations; safe caller paths show possible indirect calls separately.

Findings are a report, so a successful audit exits 0. Source errors exit 1;
invalid arguments exit 2. With no audit flag, vet performs the unsafe audit.

Usage:
  pluto vet [-unsafe] [packages] [flags]

Flags:
      --brief          print each error on one line, without its explanation (default from $PLUTO_BRIEF)
  -h, --help           help for vet
      --mode string    source build mode: hosted, freestanding or kernel (default "hosted")
      --tags strings   additional build tags
      --unsafe         audit unsafe operations and safe callers
`

// vetCommand runs pluto vet with args and returns the exit status.
func vetCommand(args []string, stdout, stderr io.Writer) int {
	if wantsHelp(args) {
		say(stdout, "%s", pu.Box(vetHelp))
		return 0
	}
	brief := "false"
	if envBrief() {
		brief = "true"
	}
	f := pu.New(goFlags{})
	f.define("unsafe", true, "false")
	f.define("brief", true, brief)
	f.define("mode", false, "hosted")
	f.define("tags", false, "")
	err := f.parse(args)
	if err == nil {
		err = checkMode(f.str("mode"))
	}
	if err == nil {
		err = runVet(f, stdout)
	}
	if err == nil {
		return 0
	}
	if ferr, ok := err.(*flagError); ok {
		say(stderr, "pluto vet: %s\n", pu.Box(ferr.msg))
		return 2
	}
	diag.Print(stderr, err, f.boolean("brief"))
	return 1
}

// runVet audits each package the arguments name, reporting each package
// once.
func runVet(f *goFlags, stdout io.Writer) error {
	mode := f.str("mode")
	var tags []string
	if t := f.str("tags"); t != "" {
		tags = compat.Split(t, ",")
	}
	targets := vetTargets(f.args) or return
	seen := map[string]bool{}
	for _, target := range targets {
		btags := load.BuildTags(mode != "hosted", mode == "kernel", tags)
		if mode == "hosted" {
			btags["gencheck"] = true
		}
		cfg := pu.New(load.Config{Tags: btags, Stdlib: target.stdlib, Sizes: types2.SizesFor("gc", "amd64")})
		prog := load.LoadPackage(cfg, target.pkg) or return
		// A pattern can select a dependency more than once. Report its
		// source only once, while retaining its nodes for call resolution.
		var selected []*load.Package
		for _, p := range prog.Packages {
			if !seen[p.Dir] {
				seen[p.Dir] = true
				selected = compat.Append(selected, p)
			}
		}
		audit.WriteSelected(stdout, prog, selected) or return
	}
	return nil
}

// A vetTarget is a package to audit: a directory or a standard library
// import path, and the standard library to load it with.
type vetTarget struct {
	pkg    string
	stdlib fs.FS
}

// vetTargets turns the command-line packages into targets, as cmd/pluto's
// expandTestTargets does.
func vetTargets(args []string) ([]vetTarget, error) {
	if len(args) == 0 {
		args = []string{"."}
	}
	var targets []vetTarget
	for _, arg := range args {
		recursive := false
		if rest, ok := strings.CutSuffix(arg, "/..."); ok {
			arg, recursive = rest, true
			if arg == "" {
				arg = "/"
			}
		} else if arg == "..." {
			arg, recursive = ".", true
		}
		if arg == "pluto" || strings.HasPrefix(arg, "pluto/") {
			lib := stdlib()
			if !recursive {
				targets = compat.Append(targets, vetTarget{arg, lib})
				continue
			}
			targets = walkLibTargets(lib, arg, targets) or return
			continue
		}
		if !recursive {
			targets = compat.Append(targets, dirVetTarget(arg))
			continue
		}
		var dirs []string
		dirs = walkSourceDirs(arg, true, dirs) or return
		for _, dir := range dirs {
			targets = compat.Append(targets, dirVetTarget(dir))
		}
	}
	return targets, nil
}

// walkLibTargets appends a target for dir of lib and each directory below
// it that holds source, in lexical order.
func walkLibTargets(lib fs.FS, dir string, targets []vetTarget) ([]vetTarget, error) {
	entries := fs.ReadDir(lib, dir) or return targets, err
	if hasSourceIn(entries) {
		targets = compat.Append(targets, vetTarget{dir, lib})
	}
	for _, e := range entries {
		if e.IsDir() {
			targets = walkLibTargets(lib, compat.Concat(dir, "/", e.Name()), targets) or return targets, err
		}
	}
	return targets, nil
}

// walkSourceDirs appends dir and each directory below it that holds source,
// in lexical order, skipping testdata and names that start with . or _
// below the root.
func walkSourceDirs(dir string, root bool, dirs []string) ([]string, error) {
	if !root {
		name := filepath.Base(dir)
		if name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			return dirs, nil
		}
	}
	entries := fs.OSReadDir(dir) or return dirs, err
	if hasSourceIn(entries) {
		dirs = compat.Append(dirs, dir)
	}
	for _, e := range entries {
		if e.IsDir() {
			dirs = walkSourceDirs(filepath.Join(dir, e.Name()), false, dirs) or return dirs, err
		}
	}
	return dirs, nil
}

// hasSourceIn reports whether entries hold a .p file.
func hasSourceIn(entries []fs.DirEntry) bool {
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".p") {
			return true
		}
	}
	return false
}

// dirVetTarget returns the target of a directory: a package of the standard
// library tree it lies in, loaded with that tree as the library, or else a
// directory loaded with the compiler's library.
func dirVetTarget(dir string) vetTarget {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return vetTarget{dir, stdlib()}
	}
	for d := filepath.Dir(abs); ; d = filepath.Dir(d) {
		if filepath.Base(d) == "pluto" {
			root := filepath.Dir(d)
			if st, err := fs.OSStat(filepath.Join(d, "rt")); err == nil && st.IsDir() {
				if rel, err := filepath.Rel(root, abs); err == nil {
					return vetTarget{filepath.ToSlash(rel), fs.DirFS(root)}
				}
			}
		}
		if filepath.Dir(d) == d {
			return vetTarget{dir, stdlib()}
		}
	}
}
