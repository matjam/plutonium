// Translated by go2pu from internal/lsp/explain.go; edited by hand since.

package lsp

import (
	"pluto/strings"
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/pu"
)

// Errors carry a code and an explanation from internal/diag. A diagnostic
// shows the first line of the message and the code; the explanation goes
// in its related information (as plain text) and in the hover over the
// error (as markdown), and a code action shows `pluto explain CODE`.

// explainCommand is the client command a code action runs to show the long
// explanation of a code; the VS Code extension registers it.
const explainCommand = "plutonium.explainError"

// explanation returns the parts of d's explanation: its text (paragraphs
// separated by "\n"), why Plutonium differs from Go, the fix, and where
// the rule is specified.
func explanation(d *diag.Diagnostic) (text, why string, fix *diag.Fix, spec string) {
	e := diag.Lookup(d.Code)
	text, fix = d.Text, d.Fix
	if text == "" && e != nil {
		text, fix = e.Text, e.Fix
	}
	if e != nil {
		why, spec = e.Why, e.Spec
	}
	return text, why, fix, spec
}

func paragraphs(s string) []string {
	var paras []string
	for len(s) > 0 {
		line, rest, _ := strings.Cut(s, "\n")
		s = rest
		if line = strings.TrimSpace(line); line != "" {
			paras = compat.Append(paras, line)
		}
	}
	return paras
}

// explainPlain returns d's explanation as one line of plain text, for the
// diagnostic's related information, or "".
func explainPlain(d *diag.Diagnostic) string {
	text, why, _, _ := explanation(d)
	return compat.Join(compat.Append(paragraphs(text), paragraphs(why)...), " ")
}

// explainMarkdown returns the hover text of an error: its code and whole
// message, the explanation, the fix and where the rule is specified.
func explainMarkdown(d *diag.Diagnostic) string {
	b := pu.Alloc[compat.Builder]()
	first, rest, _ := strings.Cut(d.Msg, "\n")
	if d.Code != "" {
		compat.Fprintfb(b, "**%s** %s\n", compat.ArgS(string(d.Code)), compat.ArgS(escapeMarkdown(first)))
	} else {
		compat.Fprintfb(b, "%s\n", compat.ArgS(escapeMarkdown(first)))
	}
	for _, line := range paragraphs(rest) {
		compat.Fprintfb(b, "\n%s\n", compat.ArgS(escapeMarkdown(line)))
	}
	text, why, fix, spec := explanation(d)
	for _, p := range compat.Append(paragraphs(text), paragraphs(why)...) {
		compat.Fprintfb(b, "\n%s\n", compat.ArgS(escapeMarkdown(p)))
	}
	if fix != nil {
		compat.Fprintfb(b, "\n%s:\n\n```%s\n%s\n```\n", compat.ArgS(escapeMarkdown(fix.Intro)), compat.ArgS(codeLang), compat.ArgS(strings.Trim(fix.Code, "\n")))
	}
	if spec != "" {
		compat.Fprintfb(b, "\nSee %s.\n", compat.ArgS(escapeMarkdown(spec)))
	}
	if diag.Lookup(d.Code) != nil {
		compat.Fprintfb(b, "\nRun `pluto explain %s` for a longer explanation.\n", compat.ArgS(string(d.Code)))
	}
	return b.String()
}

// escapeMarkdown escapes the characters that would make an error message
// render as markdown: code in messages ("*T", "a_b") is meant literally.
func escapeMarkdown(s string) string {
	var b compat.Builder
	for _, r := range s {
		if strings.ContainsRune("\\`*_[]<>#|", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// codeAction offers to explain each error in the range that has a code in
// the catalog.
func (s *server) codeAction(p *CodeActionParams) ([]CodeAction, *rpcError) {
	actions := compat.Clone([]CodeAction{})
	seen := map[string]bool{}
	for _, d := range p.Context.Diagnostics {
		if d.Source != "pluto" || seen[d.Code] || diag.Lookup(diag.Code(d.Code)) == nil {
			continue
		}
		seen[d.Code] = true
		title := compat.Sprintfb("Explain error %s", compat.ArgS(d.Code))
		actions = compat.Append(actions, CodeAction{
			Title:       title,
			Kind:        "quickfix",
			Diagnostics: compat.Clone([]Diagnostic{d}),
			Command:     pu.New(Command{Title: title, Command: explainCommand, Arguments: compat.Clone([]string{d.Code})}),
		})
	}
	return actions, nil
}

// errorHover returns the explanations of the errors at off in f, for the
// top of the hover there.
func errorHover(f *srcFile, off int) string {
	var parts []string
	for _, e := range f.pkg.errors {
		if e.path == f.path && e.start <= off && off <= max(e.end, e.start+1) {
			parts = compat.Append(parts, explainMarkdown(e.diag))
		}
	}
	return compat.Join(parts, "\n---\n\n")
}
