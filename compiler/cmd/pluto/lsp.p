// Ported by hand from cmd/pluto/lsp.go.

package main

import (
	"compiler/compat"
	"compiler/internal/lsp"
	"compiler/pu"
	"pluto/io"
	"pluto/os"
	"pluto/strconv"
	"pluto/strings"
)

const lspLong = `lsp runs a Language Server Protocol server for editors, speaking JSON-RPC
on standard input and output: diagnostics, hover, go to definition,
references, symbols, formatting, completion, signature help and rename.

The server uses $PLUTOROOT as the standard library if it is set, and
otherwise the lib directory installed beside pluto, so that go to
definition can open its files. It logs to standard error.`

const lspUsage = `Usage:
  pluto lsp [flags]

Flags:
  -h, --help      help for lsp
  -v, --verbose   log every request and how long it took
`

// lspMain runs pluto lsp with the arguments after "lsp" and returns the
// exit status.
func lspMain(args []string, stdout, stderr io.Writer) int {
	verbose, help := false, false
	var debounce compat.Duration
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			if i+1 < len(args) {
				return lspUsageError(stderr, compat.Sprintfb("unknown command %q for %q", compat.ArgS(args[i+1]), compat.ArgS("pluto lsp")))
			}
		case strings.HasPrefix(a, "--"):
			name, value, hasValue := strings.Cut(strings.SliceFrom(a, 2), "=")
			// The hidden --debounce flag sets how long the server waits
			// after an edit before it publishes diagnostics, in
			// milliseconds or with an ms suffix, for the tests that drive
			// this binary.
			if name == "debounce" {
				if !hasValue {
					if i+1 >= len(args) {
						return lspUsageError(stderr, "flag needs an argument: --debounce")
					}
					i++
					value = args[i]
				}
				n, err := strconv.Atoi(strings.TrimSuffix(value, "ms"))
				if err != nil || n <= 0 {
					return lspUsageError(stderr, compat.Sprintfb("invalid argument %q for \"--debounce\" flag", compat.ArgS(value)))
				}
				debounce = compat.Duration(n) * 1000000
				continue
			}
			var p *bool
			switch name {
			case "verbose":
				p = &verbose
			case "help":
				p = &help
			default:
				return lspUsageError(stderr, compat.Concat("unknown flag: --", name))
			}
			if !hasValue {
				value = "true"
			}
			b, ok := parseBool(value)
			if !ok {
				return lspUsageError(stderr, compat.Sprintfb("invalid argument %q for %q flag: strconv.ParseBool: parsing %q: invalid syntax", compat.ArgS(value), compat.ArgS(compat.Concat("-", strings.Slice(name, 0, 1), ", --", name)), compat.ArgS(value)))
			}
			*p = b
		case strings.HasPrefix(a, "-") && len(a) > 1:
			shorts := strings.SliceFrom(a, 1)
			for len(shorts) > 0 {
				s, at := strings.Slice(shorts, 0, 1), shorts
				shorts = strings.SliceFrom(shorts, 1)
				var p *bool
				title := ""
				switch s {
				case "v":
					p, title = &verbose, "-v, --verbose"
				case "h":
					p, title = &help, "-h, --help"
				default:
					return lspUsageError(stderr, compat.Sprintfb("unknown shorthand flag: '%s' in -%s", compat.ArgS(s), compat.ArgS(at)))
				}
				value := "true"
				if strings.HasPrefix(shorts, "=") {
					value = strings.SliceFrom(shorts, 1)
					shorts = ""
				}
				b, ok := parseBool(value)
				if !ok {
					return lspUsageError(stderr, compat.Sprintfb("invalid argument %q for %q flag: strconv.ParseBool: parsing %q: invalid syntax", compat.ArgS(value), compat.ArgS(title), compat.ArgS(value)))
				}
				*p = b
			}
		default:
			return lspUsageError(stderr, compat.Sprintfb("unknown command %q for %q", compat.ArgS(a), compat.ArgS("pluto lsp")))
		}
	}
	if help {
		say(stdout, "%s\n\n%s", lspLong, lspUsage)
		return 0
	}
	opts := lsp.Options{
		StdlibDir: stdlibDir(),
		InFd:      0,
		Debounce:  debounce,
		Version:   version,
		Log:       stderr,
		Verbose:   verbose,
	}
	if err := lsp.Serve(os.Stdin, stdout, opts); err != nil {
		_, _ = compat.Fprintfb(stderr, "%s\n", compat.ArgS(err.Error()))
		return 1
	}
	return 0
}

func lspUsageError(stderr io.Writer, msg string) int {
	say(stderr, "pluto lsp: %s\n%s", pu.Box(msg), lspUsage)
	return 2
}
