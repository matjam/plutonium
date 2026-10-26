// Translated by go2pu from internal/lsp/format.go; edited by hand since.

package lsp

import (
	"pluto/bytes"
	"selfhost/compat"
	"selfhost/internal/syntax/format"
)

// formatting formats a document as pluto fmt does. A document that does
// not parse is left alone: its syntax errors are already diagnostics.
func (s *server) formatting(p *DocumentFormattingParams) ([]TextEdit, *rpcError) {
	path := pathFromURI(p.TextDocument.URI)
	if path == "" {
		return nil, newError(codeInvalidParams, compat.Concat("not a file URI: ", p.TextDocument.URI))
	}
	src, err := s.ws.read(path)
	if err != nil {
		return nil, newError(codeRequestFailed, err.Error())
	}
	out, err := format.File(path, src, false)
	if err != nil {
		return nil, nil
	}
	return replaceEdits(src, out), nil
}

// replaceEdits returns one edit that turns src into out, replacing only the
// lines between their common first and last lines, so that the editor
// keeps the cursor and folds outside the change. It returns no edits if
// they are equal.
func replaceEdits(src, out []byte) []TextEdit {
	if bytes.Equal(src, out) {
		return compat.Clone([]TextEdit{})
	}
	// Common prefix and suffix, in whole lines.
	pre := 0
	for pre < len(src) && pre < len(out) && src[pre] == out[pre] {
		pre++
	}
	pre = bytes.LastIndexByte(src[:pre], '\n') + 1
	suf := 0
	for suf < len(src)-pre && suf < len(out)-pre && src[len(src)-1-suf] == out[len(out)-1-suf] {
		suf++
	}
	// Shorten the suffix to start at a line start in both.
	for suf > 0 && (src[len(src)-suf-1] != '\n' || out[len(out)-suf-1] != '\n') {
		suf--
	}
	t := newText(src)
	return compat.Clone([]TextEdit{{
		Range:   t.rangeOf(pre, len(src)-suf),
		NewText: compat.String(out[pre : len(out)-suf]),
	}})
}
