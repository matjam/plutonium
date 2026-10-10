package diag

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// FromTool returns the diagnostic for a failed run of an external tool:
// the assembler (as), the linker (ld) or the C compiler. err is what
// running it returned, and output its error output. rename maps the
// temporary file names the tool saw to the names the user knows.
//
// The message starts "running <tool> failed: ", followed by the cause
// when it is one of the common ones, and then the tool's output, indented.
func FromTool(tool string, err error, output string, rename map[string]string) *Diagnostic {
	for from, to := range rename {
		output = strings.ReplaceAll(output, from, to)
	}
	output = strings.TrimRight(output, "\n")
	d := &Diagnostic{Code: LinkFailed}
	cause := err.Error()
	switch {
	case errors.Is(err, exec.ErrNotFound):
		d.Code = ToolNotFound
		d.Text = fmt.Sprintf("Plutonium uses the system's %s, which is not installed or not on the PATH. On most Linux systems the binutils package provides as and ld, and a C compiler package provides cc.", toolName(tool))
	case tool == "as":
		d.Code = AssemblerError
	case tool != "ld":
		d.Code = CCompilerError
	default:
		cause, d.Code, d.Text = linkCause(output, cause)
	}
	d.Msg = fmt.Sprintf("running %s failed: %s", tool, cause)
	if output != "" {
		d.Msg += "\n\t" + strings.ReplaceAll(output, "\n", "\n\t")
	}
	return d
}

func toolName(tool string) string {
	switch tool {
	case "as":
		return "GNU assembler (as)"
	case "ld":
		return "GNU linker (ld)"
	}
	return "C compiler (" + tool + ")"
}

var (
	undefinedRef = regexp.MustCompile("undefined reference to `([^']*)'")
	inFunction   = regexp.MustCompile("in function `([^']*)'")
	relocTrunc   = regexp.MustCompile("relocation truncated to fit: (R_X86_64_[A-Z0-9]+) against (?:symbol )?`?([^' ]*)'?")
	multipleDef  = regexp.MustCompile("multiple definition of `([^']*)'")
)

// linkCause recognises the common linker failures in ld's output and
// returns the cause for the message, the code and an explanation.
func linkCause(output, status string) (string, Code, string) {
	if m := undefinedRef.FindStringSubmatch(output); m != nil {
		sym := m[1]
		user := ""
		if f := inFunction.FindStringSubmatch(output); f != nil {
			user = f[1]
		}
		var b strings.Builder
		fmt.Fprintf(&b, "The program uses the symbol %s", sym)
		if user != "" {
			fmt.Fprintf(&b, " (in %s)", user)
		}
		b.WriteString(", but nothing linked into it defines that symbol.\n")
		if strings.Contains(sym, ".") {
			fmt.Fprintf(&b, "%s looks like a Plutonium function. A function declared without a body must be defined in a .s file of its package, under the symbol \"%s\" in quotes, or be marked //pu:extern with the name of a C function.", sym, sym)
		} else {
			fmt.Fprintf(&b, "%s looks like a C symbol, as named by //pu:extern. Add the .c or .o file that defines it to the package, or check the spelling in the //pu:extern line.", sym)
		}
		return "undefined reference to " + sym, UndefinedSymbol, b.String()
	}
	if m := relocTrunc.FindStringSubmatch(output); m != nil {
		text := fmt.Sprintf("The code reaches %s with a %s relocation, a 32-bit offset, and the linker placed it too far away for that. Compiled Plutonium code expects all code and data within 2 GiB of each other: near address 0 in hosted builds, and in the top 2 GiB of the address space in kernel mode.\nThe usual cause is a linker script that places a section elsewhere. In kernel mode, link the image at or above 0xFFFFFFFF80000000, as the default kernel linker script does.", m[2], m[1])
		return "relocation truncated to fit (" + m[1] + " against " + m[2] + ")", RelocOverflow, text
	}
	if m := multipleDef.FindStringSubmatch(output); m != nil {
		text := fmt.Sprintf("Two of the objects linked into the program define %s. Rename one of them, or remove the duplicate .s, .c or .o file. A //pu:export name must not also be defined in C or assembly.", m[1])
		return "multiple definition of " + m[1], MultipleDefs, text
	}
	return status, LinkFailed, ""
}
