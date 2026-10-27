package main

// The flag parser of pluto doc and pluto vet, which cmd/pluto parses with
// Go's flag package (ContinueOnError, output discarded) rather than cobra:
// single or double dashes, -name=value or -name value, flags only before
// the first positional argument or "--", and Go's error messages.

import (
	"compiler/compat"
	"compiler/pu"
	"pluto/strings"
)

// goFlags is a set of flags and, once parsed, their values and the
// remaining positional arguments.
type goFlags struct {
	names  []string
	bools  []bool // whether the flag of the same index is a boolean
	values []string
	args   []string
}

// define adds the flag name, a boolean one if isBool, with the default
// value def.
func (f *goFlags) define(name string, isBool bool, def string) {
	f.names = compat.Append(f.names, name)
	f.bools = compat.Append(f.bools, isBool)
	f.values = compat.Append(f.values, def)
}

func (f *goFlags) index(name string) int {
	for i, n := range f.names {
		if n == name {
			return i
		}
	}
	return -1
}

// str returns the value of the flag name.
func (f *goFlags) str(name string) string { return f.values[f.index(name)] }

// boolean returns the value of the boolean flag name.
func (f *goFlags) boolean(name string) bool {
	b, _ := parseBool(f.str(name))
	return b
}

// parse parses args as Go's FlagSet.Parse does. An error is a flagError,
// which the commands print with their name and exit with status 2.
func (f *goFlags) parse(args []string) error {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) < 2 || !strings.HasPrefix(a, "-") {
			f.args = args[i:]
			return nil
		}
		dashes := 1
		if strings.HasPrefix(a, "--") {
			dashes = 2
			if len(a) == 2 {
				f.args = args[i+1:]
				return nil
			}
		}
		name := strings.SliceFrom(a, dashes)
		if strings.HasPrefix(name, "-") || strings.HasPrefix(name, "=") {
			return pu.New(flagError{msg: compat.Concat("bad flag syntax: ", a)})
		}
		name, value, hasValue := strings.Cut(name, "=")
		k := f.index(name)
		if k < 0 {
			return pu.New(flagError{msg: compat.Concat("flag provided but not defined: -", name)})
		}
		if f.bools[k] {
			if !hasValue {
				value = "true"
			} else if _, ok := parseBool(value); !ok {
				return pu.New(flagError{msg: compat.Sprintfb("invalid boolean value %q for -%s: parse error", compat.ArgS(value), compat.ArgS(name))})
			}
		} else if !hasValue {
			if i+1 >= len(args) {
				return pu.New(flagError{msg: compat.Concat("flag needs an argument: -", name)})
			}
			i++
			value = args[i]
		}
		f.values[k] = value
	}
	f.args = nil
	return nil
}

// wantsHelp reports whether args ask for a command's help, as cmd/pluto's
// doc and vet check before parsing their flags.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "-help" || a == "--help" {
			return true
		}
	}
	return false
}

// checkMode returns the error of a build mode that is not hosted,
// freestanding or kernel.
func checkMode(mode string) error {
	if mode != "hosted" && mode != "freestanding" && mode != "kernel" {
		return flagErrorf("unknown mode %q (supported: hosted, freestanding, kernel)", pu.Box(mode))
	}
	return nil
}
