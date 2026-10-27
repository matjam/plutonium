// Translated by go2pu from internal/lsp/protocol.go; edited by hand since.
// The Go types' json tags became the encoders and decoders below.

package lsp

import (
	"compiler/compat"
	"compiler/pu"
)

// The subset of the LSP 3.17 protocol that the server implements. Field
// names follow the specification.

type Position struct {
	Line      int
	Character int // UTF-16 code units
}

type Range struct {
	Start Position
	End   Position
}

type Location struct {
	URI   string
	Range Range
}

type TextDocumentIdentifier struct {
	URI string
}

type TextDocumentItem struct {
	URI        string
	LanguageID string
	Version    int
	Text       string
}

type VersionedTextDocumentIdentifier struct {
	URI     string
	Version int
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier
	Position     Position
}

type WorkspaceFolder struct {
	URI  string
	Name string
}

type InitializeParams struct {
	RootURI               string
	RootPath              string
	WorkspaceFolders      []WorkspaceFolder
	InitializationOptions *jsonValue
}

// Settings are the server's options, passed as initializationOptions or
// as the "plutonium" section of workspace/didChangeConfiguration.
type Settings struct {
	Target string   // x86_64-linux (default) or x86_64-none
	Mode   string   // hosted, freestanding or kernel; default from the target
	Tags   []string // additional build tags
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem
}

type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier
	ContentChanges []TextDocumentContentChangeEvent
}

// TextDocumentContentChangeEvent is a change with full document sync: the
// whole new text. Range is set only by clients that ignore the sync kind.
type TextDocumentContentChangeEvent struct {
	Range    Range
	HasRange bool
	Text     string
}

type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier
	Text         string
	HasText      bool
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier
}

type DidChangeConfigurationParams struct {
	Settings *jsonValue
}

type DidChangeWatchedFilesParams struct {
	Changes []FileEvent
}

type FileEvent struct {
	URI  string
	Type int
}

type DidChangeWorkspaceFoldersParams struct {
	Event WorkspaceFoldersChangeEvent
}

type WorkspaceFoldersChangeEvent struct {
	Added   []WorkspaceFolder
	Removed []WorkspaceFolder
}

// Diagnostics.

const (
	SeverityError       = 1
	SeverityWarning     = 2
	SeverityInformation = 3
	SeverityHint        = 4
)

type Diagnostic struct {
	Range              Range
	Severity           int
	Code               string
	CodeDescription    *CodeDescription
	Source             string
	Message            string
	RelatedInformation []DiagnosticRelatedInformation
}

type CodeDescription struct {
	Href string
}

type DiagnosticRelatedInformation struct {
	Location Location
	Message  string
}

type PublishDiagnosticsParams struct {
	URI         string
	Version     int
	HasVersion  bool
	Diagnostics []Diagnostic
}

// Hover.

type MarkupContent struct {
	Kind  string // "markdown" or "plaintext"
	Value string
}

type Hover struct {
	Contents MarkupContent
	Range    *Range
}

// References.

type ReferenceParams struct {
	TextDocumentPositionParams
	Context ReferenceContext
}

type ReferenceContext struct {
	IncludeDeclaration bool
}

// Symbols.

// SymbolKind values used by the server.
const (
	SymbolFile          = 1
	SymbolPackage       = 4
	SymbolMethod        = 6
	SymbolField         = 8
	SymbolInterface     = 11
	SymbolFunction      = 12
	SymbolVariable      = 13
	SymbolConstant      = 14
	SymbolClass         = 5
	SymbolEnum          = 10
	SymbolEnumMember    = 22
	SymbolStruct        = 23
	SymbolTypeParameter = 26
)

type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier
}

type DocumentSymbol struct {
	Name           string
	Detail         string
	Kind           int
	Range          Range
	SelectionRange Range
	Children       []DocumentSymbol
}

type WorkspaceSymbolParams struct {
	Query string
}

type SymbolInformation struct {
	Name          string
	Kind          int
	Location      Location
	ContainerName string
}

// Formatting.

type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier
}

type TextEdit struct {
	Range   Range
	NewText string
}

// Completion.

// CompletionItemKind values used by the server.
const (
	CompletionMethod     = 2
	CompletionFunction   = 3
	CompletionField      = 5
	CompletionVariable   = 6
	CompletionClass      = 7
	CompletionInterface  = 8
	CompletionModule     = 9
	CompletionKeyword    = 14
	CompletionEnum       = 13
	CompletionEnumMember = 20
	CompletionConstant   = 21
	CompletionStruct     = 22
	CompletionTypeParam  = 25
)

type CompletionItem struct {
	Label         string
	Kind          int
	Detail        string
	Documentation *MarkupContent
	SortText      string
}

type CompletionList struct {
	IsIncomplete bool
	Items        []CompletionItem
}

// Signature help.

type SignatureHelp struct {
	Signatures      []SignatureInformation
	ActiveSignature int
	ActiveParameter int
}

type SignatureInformation struct {
	Label         string
	Documentation *MarkupContent
	Parameters    []ParameterInformation
}

type ParameterInformation struct {
	Label [2]int // UTF-16 offsets into the signature label
}

// Rename.

type RenameParams struct {
	TextDocumentPositionParams
	NewName string
}

// A WorkspaceEdit's changes are by document URI; uris keeps them in the
// order they were added, and encoding sorts them as encoding/json sorts a
// map's keys.
type WorkspaceEdit struct {
	uris  []string
	edits [][]TextEdit
}

// add appends an edit to the changes of the document uri.
func (e *WorkspaceEdit) add(uri string, edit TextEdit) {
	for i, u := range e.uris {
		if u == uri {
			e.edits[i] = compat.Append(e.edits[i], edit)
			return
		}
	}
	e.uris = compat.Append(e.uris, uri)
	e.edits = compat.Append(e.edits, compat.Clone([]TextEdit{edit}))
}

type PrepareRenameResult struct {
	Range       Range
	Placeholder string
}

type ShowMessageParams struct {
	Type    int // 1 error, 2 warning, 3 info, 4 log
	Message string
}

// Code actions.

type CodeActionParams struct {
	TextDocument TextDocumentIdentifier
	Range        Range
	Context      CodeActionContext
}

type CodeActionContext struct {
	Diagnostics []Diagnostic
}

type CodeAction struct {
	Title       string
	Kind        string
	Diagnostics []Diagnostic
	Command     *Command
}

type Command struct {
	Title     string
	Command   string
	Arguments []string
}

// Encoders. Each writes its value as encoding/json writes the Go type with
// its json tags.

func (p Position) write(w *jsonWriter) {
	w.beginObject()
	w.key("line")
	w.integer(p.Line)
	w.key("character")
	w.integer(p.Character)
	w.endObject()
}

func (r Range) write(w *jsonWriter) {
	w.beginObject()
	w.key("start")
	r.Start.write(w)
	w.key("end")
	r.End.write(w)
	w.endObject()
}

func (l Location) write(w *jsonWriter) {
	w.beginObject()
	w.key("uri")
	w.str(l.URI)
	w.key("range")
	l.Range.write(w)
	w.endObject()
}

func writeLocations(w *jsonWriter, locs []Location) {
	if locs == nil {
		w.null()
		return
	}
	w.beginArray()
	for _, l := range locs {
		l.write(w)
	}
	w.endArray()
}

func writeStrings(w *jsonWriter, list []string) {
	if list == nil {
		w.null()
		return
	}
	w.beginArray()
	for _, s := range list {
		w.str(s)
	}
	w.endArray()
}

// writeInitializeResult writes the result of initialize: the server's
// capabilities.
func writeInitializeResult(w *jsonWriter, version string) {
	w.beginObject()
	w.key("capabilities")
	w.beginObject()
	w.key("positionEncoding")
	w.str("utf-16")
	w.key("textDocumentSync")
	w.beginObject()
	w.key("openClose")
	w.boolean(true)
	w.key("change")
	w.integer(1)
	w.key("save")
	w.beginObject()
	w.key("includeText")
	w.boolean(false)
	w.endObject()
	w.endObject()
	for _, name := range []string{"hoverProvider", "definitionProvider", "referencesProvider", "documentSymbolProvider", "workspaceSymbolProvider", "documentFormattingProvider"} {
		w.key(name)
		w.boolean(true)
	}
	w.key("completionProvider")
	w.beginObject()
	w.key("triggerCharacters")
	writeStrings(w, compat.Clone([]string{"."}))
	w.endObject()
	w.key("signatureHelpProvider")
	w.beginObject()
	w.key("triggerCharacters")
	writeStrings(w, compat.Clone([]string{"(", ","}))
	w.key("retriggerCharacters")
	writeStrings(w, compat.Clone([]string{","}))
	w.endObject()
	w.key("renameProvider")
	w.beginObject()
	w.key("prepareProvider")
	w.boolean(true)
	w.endObject()
	w.key("codeActionProvider")
	w.beginObject()
	w.key("codeActionKinds")
	writeStrings(w, compat.Clone([]string{"quickfix"}))
	w.endObject()
	w.key("workspace")
	w.beginObject()
	w.key("workspaceFolders")
	w.beginObject()
	w.key("supported")
	w.boolean(true)
	w.key("changeNotifications")
	w.boolean(true)
	w.endObject()
	w.endObject()
	w.endObject()
	w.key("serverInfo")
	w.beginObject()
	w.key("name")
	w.str("pluto lsp")
	if version != "" {
		w.key("version")
		w.str(version)
	}
	w.endObject()
	w.endObject()
}

func (d *Diagnostic) write(w *jsonWriter) {
	w.beginObject()
	w.key("range")
	d.Range.write(w)
	w.key("severity")
	w.integer(d.Severity)
	if d.Code != "" {
		w.key("code")
		w.str(d.Code)
	}
	if d.CodeDescription != nil {
		w.key("codeDescription")
		w.beginObject()
		w.key("href")
		w.str(d.CodeDescription.Href)
		w.endObject()
	}
	w.key("source")
	w.str(d.Source)
	w.key("message")
	w.str(d.Message)
	if len(d.RelatedInformation) > 0 {
		w.key("relatedInformation")
		w.beginArray()
		for _, r := range d.RelatedInformation {
			w.beginObject()
			w.key("location")
			r.Location.write(w)
			w.key("message")
			w.str(r.Message)
			w.endObject()
		}
		w.endArray()
	}
	w.endObject()
}

func writeDiagnostics(w *jsonWriter, diags []Diagnostic) {
	if diags == nil {
		w.null()
		return
	}
	w.beginArray()
	for i := range diags {
		diags[i].write(w)
	}
	w.endArray()
}

func (p *PublishDiagnosticsParams) write(w *jsonWriter) {
	w.beginObject()
	w.key("uri")
	w.str(p.URI)
	if p.HasVersion {
		w.key("version")
		w.integer(p.Version)
	}
	w.key("diagnostics")
	if p.Diagnostics == nil {
		w.beginArray() // publish sends [] for none
		w.endArray()
	} else {
		writeDiagnostics(w, p.Diagnostics)
	}
	w.endObject()
}

func (m MarkupContent) write(w *jsonWriter) {
	w.beginObject()
	w.key("kind")
	w.str(m.Kind)
	w.key("value")
	w.str(m.Value)
	w.endObject()
}

func (h *Hover) write(w *jsonWriter) {
	if h == nil {
		w.null()
		return
	}
	w.beginObject()
	w.key("contents")
	h.Contents.write(w)
	if h.Range != nil {
		w.key("range")
		h.Range.write(w)
	}
	w.endObject()
}

func writeDocumentSymbols(w *jsonWriter, syms []DocumentSymbol) {
	if syms == nil {
		w.null()
		return
	}
	w.beginArray()
	for i := range syms {
		s := &syms[i]
		w.beginObject()
		w.key("name")
		w.str(s.Name)
		if s.Detail != "" {
			w.key("detail")
			w.str(s.Detail)
		}
		w.key("kind")
		w.integer(s.Kind)
		w.key("range")
		s.Range.write(w)
		w.key("selectionRange")
		s.SelectionRange.write(w)
		if len(s.Children) > 0 {
			w.key("children")
			writeDocumentSymbols(w, s.Children)
		}
		w.endObject()
	}
	w.endArray()
}

func writeSymbolInformation(w *jsonWriter, syms []SymbolInformation) {
	if syms == nil {
		w.null()
		return
	}
	w.beginArray()
	for i := range syms {
		s := &syms[i]
		w.beginObject()
		w.key("name")
		w.str(s.Name)
		w.key("kind")
		w.integer(s.Kind)
		w.key("location")
		s.Location.write(w)
		if s.ContainerName != "" {
			w.key("containerName")
			w.str(s.ContainerName)
		}
		w.endObject()
	}
	w.endArray()
}

func writeTextEdits(w *jsonWriter, edits []TextEdit) {
	if edits == nil {
		w.null()
		return
	}
	w.beginArray()
	for _, e := range edits {
		w.beginObject()
		w.key("range")
		e.Range.write(w)
		w.key("newText")
		w.str(e.NewText)
		w.endObject()
	}
	w.endArray()
}

func (l *CompletionList) write(w *jsonWriter) {
	if l == nil {
		w.null()
		return
	}
	w.beginObject()
	w.key("isIncomplete")
	w.boolean(l.IsIncomplete)
	w.key("items")
	if l.Items == nil {
		w.null()
	} else {
		w.beginArray()
		for i := range l.Items {
			it := &l.Items[i]
			w.beginObject()
			w.key("label")
			w.str(it.Label)
			if it.Kind != 0 {
				w.key("kind")
				w.integer(it.Kind)
			}
			if it.Detail != "" {
				w.key("detail")
				w.str(it.Detail)
			}
			if it.Documentation != nil {
				w.key("documentation")
				it.Documentation.write(w)
			}
			if it.SortText != "" {
				w.key("sortText")
				w.str(it.SortText)
			}
			w.endObject()
		}
		w.endArray()
	}
	w.endObject()
}

func (h *SignatureHelp) write(w *jsonWriter) {
	if h == nil {
		w.null()
		return
	}
	w.beginObject()
	w.key("signatures")
	if h.Signatures == nil {
		w.null()
	} else {
		w.beginArray()
		for i := range h.Signatures {
			s := &h.Signatures[i]
			w.beginObject()
			w.key("label")
			w.str(s.Label)
			if s.Documentation != nil {
				w.key("documentation")
				s.Documentation.write(w)
			}
			w.key("parameters")
			if s.Parameters == nil {
				w.null()
			} else {
				w.beginArray()
				for _, p := range s.Parameters {
					w.beginObject()
					w.key("label")
					w.beginArray()
					w.integer(p.Label[0])
					w.integer(p.Label[1])
					w.endArray()
					w.endObject()
				}
				w.endArray()
			}
			w.endObject()
		}
		w.endArray()
	}
	w.key("activeSignature")
	w.integer(h.ActiveSignature)
	w.key("activeParameter")
	w.integer(h.ActiveParameter)
	w.endObject()
}

func (e *WorkspaceEdit) write(w *jsonWriter) {
	if e == nil {
		w.null()
		return
	}
	// The URIs in sorted order, as encoding/json writes a map.
	order := compat.Make[int](len(e.uris))
	for i := range order {
		order[i] = i
	}
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && e.uris[order[j]] < e.uris[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	w.beginObject()
	w.key("changes")
	w.beginObject()
	for _, i := range order {
		w.key(e.uris[i])
		writeTextEdits(w, e.edits[i])
	}
	w.endObject()
	w.endObject()
}

func (r *PrepareRenameResult) write(w *jsonWriter) {
	if r == nil {
		w.null()
		return
	}
	w.beginObject()
	w.key("range")
	r.Range.write(w)
	w.key("placeholder")
	w.str(r.Placeholder)
	w.endObject()
}

func (p ShowMessageParams) write(w *jsonWriter) {
	w.beginObject()
	w.key("type")
	w.integer(p.Type)
	w.key("message")
	w.str(p.Message)
	w.endObject()
}

func writeCodeActions(w *jsonWriter, actions []CodeAction) {
	if actions == nil {
		w.null()
		return
	}
	w.beginArray()
	for i := range actions {
		a := &actions[i]
		w.beginObject()
		w.key("title")
		w.str(a.Title)
		w.key("kind")
		w.str(a.Kind)
		if len(a.Diagnostics) > 0 {
			w.key("diagnostics")
			writeDiagnostics(w, a.Diagnostics)
		}
		if a.Command != nil {
			w.key("command")
			w.beginObject()
			w.key("title")
			w.str(a.Command.Title)
			w.key("command")
			w.str(a.Command.Command)
			if len(a.Command.Arguments) > 0 {
				w.key("arguments")
				writeStrings(w, a.Command.Arguments)
			}
			w.endObject()
		}
		w.endObject()
	}
	w.endArray()
}

// Decoders. Each reads a parsed value into a Go value as json.Unmarshal
// reads into the Go type with its json tags; field is the path to v, for
// errors.

func decodePosition(d *decoder, v *jsonValue, p *Position, field string) {
	if !d.object(v, "lsp.Position", field) {
		return
	}
	d.integer(v.get("line"), &p.Line, subfield(field, "line"))
	d.integer(v.get("character"), &p.Character, subfield(field, "character"))
}

func decodeRange(d *decoder, v *jsonValue, r *Range, field string) {
	if !d.object(v, "lsp.Range", field) {
		return
	}
	decodePosition(d, v.get("start"), &r.Start, subfield(field, "start"))
	decodePosition(d, v.get("end"), &r.End, subfield(field, "end"))
}

func decodeLocation(d *decoder, v *jsonValue, l *Location, field string) {
	if !d.object(v, "lsp.Location", field) {
		return
	}
	d.str(v.get("uri"), &l.URI, subfield(field, "uri"))
	decodeRange(d, v.get("range"), &l.Range, subfield(field, "range"))
}

func decodeDocumentID(d *decoder, v *jsonValue, id *TextDocumentIdentifier, field string) {
	if !d.object(v, "lsp.TextDocumentIdentifier", field) {
		return
	}
	d.str(v.get("uri"), &id.URI, subfield(field, "uri"))
}

func decodeWorkspaceFolders(d *decoder, v *jsonValue, dst *[]WorkspaceFolder, field string) {
	elems := d.array(v, "[]lsp.WorkspaceFolder", field)
	if elems == nil {
		if v.isNull() && v != nil {
			*dst = nil
		}
		return
	}
	list := compat.MakeCap[WorkspaceFolder](0, len(elems))
	for _, e := range elems {
		var f WorkspaceFolder
		if d.object(e, "lsp.WorkspaceFolder", field) {
			d.str(e.get("uri"), &f.URI, subfield(field, "uri"))
			d.str(e.get("name"), &f.Name, subfield(field, "name"))
		}
		list = compat.Append(list, f)
	}
	*dst = list
}

func decodeStrings(d *decoder, v *jsonValue, dst *[]string, field string) {
	elems := d.array(v, "[]string", field)
	if elems == nil {
		return
	}
	list := compat.MakeCap[string](0, len(elems))
	for _, e := range elems {
		var s string
		d.str(e, &s, field)
		list = compat.Append(list, s)
	}
	*dst = list
}

func decodeSettings(d *decoder, v *jsonValue, s *Settings, field string) {
	if !d.object(v, "lsp.Settings", field) {
		return
	}
	d.str(v.get("target"), &s.Target, subfield(field, "target"))
	d.str(v.get("mode"), &s.Mode, subfield(field, "mode"))
	decodeStrings(d, v.get("tags"), &s.Tags, subfield(field, "tags"))
}

func decodeInitializeParams(d *decoder, v *jsonValue, p *InitializeParams) {
	if !d.object(v, "lsp.InitializeParams", "") {
		return
	}
	if id := v.get("processId"); !id.isNull() {
		var n int
		d.integer(id, &n, "InitializeParams.processId")
	}
	d.str(v.get("rootUri"), &p.RootURI, "InitializeParams.rootUri")
	d.str(v.get("rootPath"), &p.RootPath, "InitializeParams.rootPath")
	decodeWorkspaceFolders(d, v.get("workspaceFolders"), &p.WorkspaceFolders, "InitializeParams.workspaceFolders")
	p.InitializationOptions = v.get("initializationOptions")
}

func decodePositionParams(d *decoder, v *jsonValue, p *TextDocumentPositionParams, typ string) {
	if !d.object(v, compat.Concat("lsp.", typ), "") {
		return
	}
	decodeDocumentID(d, v.get("textDocument"), &p.TextDocument, compat.Concat(typ, ".textDocument"))
	decodePosition(d, v.get("position"), &p.Position, compat.Concat(typ, ".position"))
}

func decodeReferenceParams(d *decoder, v *jsonValue, p *ReferenceParams) {
	decodePositionParams(d, v, &p.TextDocumentPositionParams, "ReferenceParams")
	if v.isNull() || v.kind != jsonObject {
		return
	}
	if c := v.get("context"); d.object(c, "lsp.ReferenceContext", "ReferenceParams.context") {
		d.boolean(c.get("includeDeclaration"), &p.Context.IncludeDeclaration, "ReferenceParams.context.includeDeclaration")
	}
}

func decodeRenameParams(d *decoder, v *jsonValue, p *RenameParams) {
	decodePositionParams(d, v, &p.TextDocumentPositionParams, "RenameParams")
	if v.isNull() || v.kind != jsonObject {
		return
	}
	d.str(v.get("newName"), &p.NewName, "RenameParams.newName")
}

func decodeDocumentParams(d *decoder, v *jsonValue, id *TextDocumentIdentifier, typ string) {
	if !d.object(v, compat.Concat("lsp.", typ), "") {
		return
	}
	decodeDocumentID(d, v.get("textDocument"), id, compat.Concat(typ, ".textDocument"))
}

func decodeWorkspaceSymbolParams(d *decoder, v *jsonValue, p *WorkspaceSymbolParams) {
	if !d.object(v, "lsp.WorkspaceSymbolParams", "") {
		return
	}
	d.str(v.get("query"), &p.Query, "WorkspaceSymbolParams.query")
}

func decodeDiagnostic(d *decoder, v *jsonValue, diag *Diagnostic, field string) {
	if !d.object(v, "lsp.Diagnostic", field) {
		return
	}
	decodeRange(d, v.get("range"), &diag.Range, subfield(field, "range"))
	d.integer(v.get("severity"), &diag.Severity, subfield(field, "severity"))
	d.str(v.get("code"), &diag.Code, subfield(field, "code"))
	if cd := v.get("codeDescription"); !cd.isNull() {
		desc := pu.New(CodeDescription{})
		if d.object(cd, "lsp.CodeDescription", subfield(field, "codeDescription")) {
			d.str(cd.get("href"), &desc.Href, subfield(field, "codeDescription.href"))
		}
		diag.CodeDescription = desc
	}
	d.str(v.get("source"), &diag.Source, subfield(field, "source"))
	d.str(v.get("message"), &diag.Message, subfield(field, "message"))
	rel := subfield(field, "relatedInformation")
	if elems := d.array(v.get("relatedInformation"), "[]lsp.DiagnosticRelatedInformation", rel); elems != nil {
		list := compat.MakeCap[DiagnosticRelatedInformation](0, len(elems))
		for _, e := range elems {
			var r DiagnosticRelatedInformation
			if d.object(e, "lsp.DiagnosticRelatedInformation", rel) {
				decodeLocation(d, e.get("location"), &r.Location, subfield(rel, "location"))
				d.str(e.get("message"), &r.Message, subfield(rel, "message"))
			}
			list = compat.Append(list, r)
		}
		diag.RelatedInformation = list
	}
}

func decodeCodeActionParams(d *decoder, v *jsonValue, p *CodeActionParams) {
	if !d.object(v, "lsp.CodeActionParams", "") {
		return
	}
	decodeDocumentID(d, v.get("textDocument"), &p.TextDocument, "CodeActionParams.textDocument")
	decodeRange(d, v.get("range"), &p.Range, "CodeActionParams.range")
	c := v.get("context")
	if !d.object(c, "lsp.CodeActionContext", "CodeActionParams.context") {
		return
	}
	const field = "CodeActionParams.context.diagnostics"
	elems := d.array(c.get("diagnostics"), "[]lsp.Diagnostic", field)
	if elems == nil {
		return
	}
	list := compat.MakeCap[Diagnostic](0, len(elems))
	for _, e := range elems {
		var diag Diagnostic
		decodeDiagnostic(d, e, &diag, field)
		list = compat.Append(list, diag)
	}
	p.Context.Diagnostics = list
}

func decodeDidOpen(d *decoder, v *jsonValue, p *DidOpenTextDocumentParams) {
	if !d.object(v, "lsp.DidOpenTextDocumentParams", "") {
		return
	}
	const field = "DidOpenTextDocumentParams.textDocument"
	t := v.get("textDocument")
	if !d.object(t, "lsp.TextDocumentItem", field) {
		return
	}
	d.str(t.get("uri"), &p.TextDocument.URI, subfield(field, "uri"))
	d.str(t.get("languageId"), &p.TextDocument.LanguageID, subfield(field, "languageId"))
	d.integer(t.get("version"), &p.TextDocument.Version, subfield(field, "version"))
	d.str(t.get("text"), &p.TextDocument.Text, subfield(field, "text"))
}

func decodeDidChange(d *decoder, v *jsonValue, p *DidChangeTextDocumentParams) {
	if !d.object(v, "lsp.DidChangeTextDocumentParams", "") {
		return
	}
	const docField = "DidChangeTextDocumentParams.textDocument"
	if t := v.get("textDocument"); d.object(t, "lsp.VersionedTextDocumentIdentifier", docField) {
		d.str(t.get("uri"), &p.TextDocument.URI, subfield(docField, "uri"))
		d.integer(t.get("version"), &p.TextDocument.Version, subfield(docField, "version"))
	}
	const field = "DidChangeTextDocumentParams.contentChanges"
	elems := d.array(v.get("contentChanges"), "[]lsp.TextDocumentContentChangeEvent", field)
	if elems == nil {
		return
	}
	list := compat.MakeCap[TextDocumentContentChangeEvent](0, len(elems))
	for _, e := range elems {
		var c TextDocumentContentChangeEvent
		if d.object(e, "lsp.TextDocumentContentChangeEvent", field) {
			if r := e.get("range"); !r.isNull() {
				c.HasRange = true
				decodeRange(d, r, &c.Range, subfield(field, "range"))
			}
			d.str(e.get("text"), &c.Text, subfield(field, "text"))
		}
		list = compat.Append(list, c)
	}
	p.ContentChanges = list
}

func decodeDidSave(d *decoder, v *jsonValue, p *DidSaveTextDocumentParams) {
	if !d.object(v, "lsp.DidSaveTextDocumentParams", "") {
		return
	}
	decodeDocumentID(d, v.get("textDocument"), &p.TextDocument, "DidSaveTextDocumentParams.textDocument")
	if t := v.get("text"); !t.isNull() {
		p.HasText = true
		d.str(t, &p.Text, "DidSaveTextDocumentParams.text")
	}
}

func decodeDidChangeConfiguration(d *decoder, v *jsonValue, p *DidChangeConfigurationParams) {
	if !d.object(v, "lsp.DidChangeConfigurationParams", "") {
		return
	}
	p.Settings = v.get("settings")
}

func decodeDidChangeWatchedFiles(d *decoder, v *jsonValue, p *DidChangeWatchedFilesParams) {
	if !d.object(v, "lsp.DidChangeWatchedFilesParams", "") {
		return
	}
	const field = "DidChangeWatchedFilesParams.changes"
	elems := d.array(v.get("changes"), "[]lsp.FileEvent", field)
	if elems == nil {
		return
	}
	list := compat.MakeCap[FileEvent](0, len(elems))
	for _, e := range elems {
		var ev FileEvent
		if d.object(e, "lsp.FileEvent", field) {
			d.str(e.get("uri"), &ev.URI, subfield(field, "uri"))
			d.integer(e.get("type"), &ev.Type, subfield(field, "type"))
		}
		list = compat.Append(list, ev)
	}
	p.Changes = list
}

func decodeDidChangeWorkspaceFolders(d *decoder, v *jsonValue, p *DidChangeWorkspaceFoldersParams) {
	if !d.object(v, "lsp.DidChangeWorkspaceFoldersParams", "") {
		return
	}
	const field = "DidChangeWorkspaceFoldersParams.event"
	e := v.get("event")
	if !d.object(e, "lsp.WorkspaceFoldersChangeEvent", field) {
		return
	}
	decodeWorkspaceFolders(d, e.get("added"), &p.Event.Added, subfield(field, "added"))
	decodeWorkspaceFolders(d, e.get("removed"), &p.Event.Removed, subfield(field, "removed"))
}
