package lsp

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// diagnose checks the package of every open document and publishes the
// diagnostics of their files that changed since they were last published.
func (s *server) diagnose() {
	start := time.Now()
	byFile := map[string][]Diagnostic{}
	files := map[string]bool{} // files whose diagnostics are known
	seen := map[*pkg]bool{}
	paths := make([]string, 0, len(s.docs))
	for path := range s.docs {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range paths {
		f, p, err := s.ws.fileOf(path)
		if err != nil {
			files[path] = true
			byFile[path] = append(byFile[path], Diagnostic{
				Severity: SeverityError, Source: "pluto",
				Message: err.Error(),
			})
			continue
		}
		if f == nil {
			mode, _ := s.ws.modeFor(path)
			files[path] = true
			byFile[path] = append(byFile[path], Diagnostic{
				Severity: SeverityInformation, Source: "pluto",
				Message: fmt.Sprintf("this file is not part of the %s build of its package (its file name or //pu:build line excludes it); set plutonium.target or plutonium.mode to check it", mode.name),
			})
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		for _, sf := range p.files {
			files[sf.path] = true
		}
		texts := map[string]*text{}
		for _, sf := range p.files {
			texts[sf.path] = sf.text
		}
		for _, e := range p.errors {
			t := texts[e.path]
			if t == nil {
				src, err := s.ws.read(e.path)
				if err != nil {
					continue
				}
				t = newText(src)
				texts[e.path] = t
			}
			files[e.path] = true
			byFile[e.path] = append(byFile[e.path], diagnosticOf(e, t))
		}
	}
	// Files no open document's package includes any more lose their
	// diagnostics: nothing would update them.
	for path := range s.published {
		files[path] = true
	}
	for _, path := range slices.Sorted(maps.Keys(files)) {
		s.publish(path, byFile[path])
	}
	s.log.Debug("diagnosed", "packages", len(seen), "took", time.Since(start))
}

// diagnosticOf converts a check error: the first line of its message, its
// code, and as related information at the same place, the rest of the
// message and the explanation. Hovering over the error shows all of it as
// markdown.
func diagnosticOf(e checkError, t *text) Diagnostic {
	first, rest, _ := strings.Cut(e.diag.Msg, "\n")
	d := Diagnostic{
		Range:    t.rangeOf(e.start, max(e.end, e.start)),
		Severity: SeverityError,
		Source:   "pluto",
		Code:     string(e.diag.Code),
		Message:  first,
	}
	at := Location{URI: uriFromPath(e.path), Range: d.Range}
	for _, line := range paragraphs(rest) {
		d.RelatedInformation = append(d.RelatedInformation, DiagnosticRelatedInformation{Location: at, Message: line})
	}
	if text := explainPlain(e.diag); text != "" {
		d.RelatedInformation = append(d.RelatedInformation, DiagnosticRelatedInformation{Location: at, Message: text})
	}
	return d
}

// publish sends the diagnostics of a file if they differ from what was
// last sent.
func (s *server) publish(path string, diags []Diagnostic) {
	if diags == nil {
		diags = []Diagnostic{}
	}
	key, _ := json.Marshal(diags)
	last, sent := s.published[path]
	open := s.docs[path] != nil
	if len(diags) == 0 && !open {
		delete(s.published, path)
	} else {
		s.published[path] = string(key)
	}
	if sent && last == string(key) || !sent && len(diags) == 0 && !open {
		return
	}
	params := PublishDiagnosticsParams{URI: uriFromPath(path), Diagnostics: diags}
	if doc := s.docs[path]; doc != nil {
		params.URI = doc.uri
		v := doc.version
		params.Version = &v
	}
	if err := s.out.notify("textDocument/publishDiagnostics", params); err != nil {
		s.log.Error("publishing diagnostics", "file", filepath.Base(path), "error", err)
	}
}
