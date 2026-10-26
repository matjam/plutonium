package tests

import (
	"regexp"
	"strings"
)

// A diagnostic is one error in pluto's long output: its position, its
// message and its code.
type diagnostic struct {
	pos, msg, code string
}

var (
	// diagHead matches the first line of a diagnostic, at the start of a
	// line: "file:line:col: message".
	diagHead = regexp.MustCompile(`^(\S.*?:\d+:\d+): (.*)$`)
	// diagCode matches the last line of a diagnostic's explanation, which
	// names its code.
	diagCode = regexp.MustCompile(`\(run "pluto explain (E\d+)" for a longer explanation\)$`)
)

// diagnostics reads the diagnostics of pluto's output in its long form: a
// line "file:line:col: message", then indented lines of explanation, the
// last of which names the code. A diagnostic without a code has code "".
func diagnostics(out string) []diagnostic {
	var ds []diagnostic
	for _, line := range strings.Split(out, "\n") {
		if m := diagHead.FindStringSubmatch(line); m != nil {
			ds = append(ds, diagnostic{pos: m[1], msg: m[2]})
			continue
		}
		if m := diagCode.FindStringSubmatch(line); m != nil && len(ds) > 0 && strings.HasPrefix(line, " ") {
			ds[len(ds)-1].code = m[1]
		}
	}
	return ds
}
