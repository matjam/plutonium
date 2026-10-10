package lsp

import (
	"fmt"
	"slices"
	"strings"

	"github.com/matjam/plutonium/internal/diag"
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
	for p := range strings.SplitSeq(s, "\n") {
		if p = strings.TrimSpace(p); p != "" {
			paras = append(paras, p)
		}
	}
	return paras
}

// explainPlain returns d's explanation as one line of plain text, for the
// diagnostic's related information, or "".
func explainPlain(d *diag.Diagnostic) string {
	text, why, _, _ := explanation(d)
	return strings.Join(slices.Concat(paragraphs(text), paragraphs(why)), " ")
}

// explainMarkdown returns the hover text of an error: its code and whole
// message, the explanation, the fix and where the rule is specified.
func explainMarkdown(d *diag.Diagnostic) string {
	var b strings.Builder
	first, rest, _ := strings.Cut(d.Msg, "\n")
	if d.Code != "" {
		fmt.Fprintf(&b, "**%s** %s\n", d.Code, escapeMarkdown(first))
	} else {
		fmt.Fprintf(&b, "%s\n", escapeMarkdown(first))
	}
	for _, line := range paragraphs(rest) {
		fmt.Fprintf(&b, "\n%s\n", escapeMarkdown(line))
	}
	text, why, fix, spec := explanation(d)
	for _, p := range slices.Concat(paragraphs(text), paragraphs(why)) {
		fmt.Fprintf(&b, "\n%s\n", escapeMarkdown(p))
	}
	if fix != nil {
		fmt.Fprintf(&b, "\n%s:\n\n```%s\n%s\n```\n", escapeMarkdown(fix.Intro), codeLang, strings.Trim(fix.Code, "\n"))
	}
	if spec != "" {
		fmt.Fprintf(&b, "\nSee %s.\n", escapeMarkdown(spec))
	}
	if diag.Lookup(d.Code) != nil {
		fmt.Fprintf(&b, "\nRun `pluto explain %s` for a longer explanation.\n", d.Code)
	}
	return b.String()
}

// escapeMarkdown escapes the characters that would make an error message
// render as markdown: code in messages ("*T", "a_b") is meant literally.
func escapeMarkdown(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\`*_[]<>#|", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// CodeActionParams and the types below are the code action request.
type CodeActionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        Range                  `json:"range"`
	Context      CodeActionContext      `json:"context"`
}

type CodeActionContext struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type CodeAction struct {
	Title       string       `json:"title"`
	Kind        string       `json:"kind"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Command     *Command     `json:"command,omitempty"`
}

type Command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}

// codeAction offers to explain each error in the range that has a code in
// the catalog.
func (s *server) codeAction(p *CodeActionParams) ([]CodeAction, *rpcError) {
	actions := []CodeAction{}
	seen := map[string]bool{}
	for _, d := range p.Context.Diagnostics {
		if d.Source != "pluto" || seen[d.Code] || diag.Lookup(diag.Code(d.Code)) == nil {
			continue
		}
		seen[d.Code] = true
		title := fmt.Sprintf("Explain error %s", d.Code)
		actions = append(actions, CodeAction{
			Title:       title,
			Kind:        "quickfix",
			Diagnostics: []Diagnostic{d},
			Command:     &Command{Title: title, Command: explainCommand, Arguments: []any{d.Code}},
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
			parts = append(parts, explainMarkdown(e.diag))
		}
	}
	return strings.Join(parts, "\n---\n\n")
}
