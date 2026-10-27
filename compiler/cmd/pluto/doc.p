package main

// pluto doc, as cmd/pluto/doc.go has it.

import (
	"pluto/io"
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/doc"
	"compiler/internal/load"
	"compiler/internal/types2"
	"compiler/pu"
)

const docHelp = `Show source documentation and declarations without running or linking the package.
The package defaults to the current directory. Listings show exported names;
-all includes private names. An explicit symbol may name a private declaration.
Types are labelled value, view, owner (moves), or counted.

Usage:
  pluto doc [-all] [-tags=tags] [package-or-directory] [symbol[.method]] [flags]

Flags:
      --all           include private names
      --brief         print each error on one line, without its explanation (default from $PLUTO_BRIEF)
  -h, --help          help for doc
      --mode string   source build mode: hosted, freestanding or kernel (default "hosted")
      --tags string   additional build tags
`

// docCommand runs pluto doc with args and returns the exit status.
func docCommand(args []string, stdout, stderr io.Writer) int {
	if wantsHelp(args) {
		say(stdout, "%s", pu.Box(docHelp))
		return 0
	}
	brief := "false"
	if envBrief() {
		brief = "true"
	}
	f := pu.New(goFlags{})
	f.define("all", true, "false")
	f.define("mode", false, "hosted")
	f.define("tags", false, "")
	f.define("brief", true, brief)
	err := f.parse(args)
	if err == nil && len(f.args) > 2 {
		err = flagErrorf("expected a package and an optional symbol")
	}
	mode := f.str("mode")
	if err == nil {
		err = checkMode(mode)
	}
	if err == nil {
		err = runDoc(f, stdout)
	}
	if err == nil {
		return 0
	}
	if ferr, ok := err.(*flagError); ok {
		say(stderr, "pluto doc: %s\n", pu.Box(ferr.msg))
		return 2
	}
	diag.Print(stderr, err, f.boolean("brief"))
	return 1
}

// runDoc loads the package the flags name and writes its documentation.
func runDoc(f *goFlags, stdout io.Writer) error {
	target, symbol := ".", ""
	if len(f.args) > 0 {
		target = f.args[0]
	}
	if len(f.args) > 1 {
		symbol = f.args[1]
	}
	mode := f.str("mode")
	tags := load.BuildTags(mode != "hosted", mode == "kernel", compat.Split(f.str("tags"), ","))
	if mode == "hosted" {
		tags["gencheck"] = true
	}
	cfg := pu.New(load.Config{Stdlib: stdlib(), Sizes: types2.SizesFor("gc", "amd64"), Tags: tags})
	prog := load.LoadPackage(cfg, target) or return
	return doc.Write(stdout, prog, symbol, f.boolean("all"))
}
