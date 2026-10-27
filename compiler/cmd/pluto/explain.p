package main

// pluto explain, as cmd/pluto/explain.go has it.

import (
	"pluto/io"
	"pluto/strconv"
	"pluto/strings"
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/pu"
)

const explainLong = `explain prints the long explanation of a compile error code, with an
example. Every compile error ends with the code to look up, such as
E0024. With no code, explain lists every code and its name.`

const explainUsage = `Usage:
  pluto explain [code...] [flags]

Flags:
  -h, --help   help for explain
`

// explainCommand runs pluto explain with args and returns the exit status.
func explainCommand(args []string, stdout, stderr io.Writer) int {
	var codes []string
	for i, a := range args {
		if a == "--" {
			codes = compat.Append(codes, args[i+1:]...)
			break
		}
		if a == "-h" || a == "--help" {
			say(stdout, "%s\n\n%s", pu.Box(explainLong), pu.Box(explainUsage))
			return 0
		}
		if strings.HasPrefix(a, "--") {
			name, _, _ := strings.Cut(strings.SliceFrom(a, 2), "=")
			say(stderr, "pluto explain: unknown flag: --%s\n%s", pu.Box(name), pu.Box(explainUsage))
			return 2
		}
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			say(stderr, "pluto explain: unknown shorthand flag: '%s' in %s\n%s", pu.Box(strings.Slice(a, 1, 2)), pu.Box(a), pu.Box(explainUsage))
			return 2
		}
		codes = compat.Append(codes, a)
	}
	if err := explain(stdout, codes); err != nil {
		diag.Print(stderr, err, envBrief())
		return 1
	}
	return 0
}

// explain writes the explanation of each code in codes, or the list of
// codes if there are none.
func explain(w io.Writer, codes []string) error {
	if len(codes) == 0 {
		for _, c := range diag.Codes() {
			say(w, "%s  %s\n", pu.Box(string(c)), pu.Box(diag.Lookup(c).Name))
		}
		return nil
	}
	for i, c := range codes {
		text := diag.Explain(normalizeCode(c)) or return
		if i > 0 {
			say(w, "\n")
		}
		say(w, "%s", pu.Box(text))
	}
	return nil
}

// normalizeCode accepts a code as printed (E0024), in lower case, or as a
// bare number (24).
func normalizeCode(s string) diag.Code {
	s = strings.ToUpper(strings.TrimSpace(s))
	if n, err := strconv.Atoi(strings.TrimPrefix(s, "E")); err == nil && n >= 0 {
		return diag.Code(compat.Sprintfb("E%04d", compat.ArgI(int64(n))))
	}
	return diag.Code(s)
}
