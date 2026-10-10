package lsp

import "encoding/json"

// The subset of the LSP 3.17 protocol that the server implements. Field
// names follow the specification.

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"` // UTF-16 code units
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type InitializeParams struct {
	ProcessID             *int              `json:"processId"`
	RootURI               string            `json:"rootUri,omitempty"`
	RootPath              string            `json:"rootPath,omitempty"`
	WorkspaceFolders      []WorkspaceFolder `json:"workspaceFolders,omitempty"`
	InitializationOptions json.RawMessage   `json:"initializationOptions,omitempty"`
}

// Settings are the server's options, passed as initializationOptions or
// as the "plutonium" section of workspace/didChangeConfiguration.
type Settings struct {
	Target string   `json:"target,omitempty"` // x86_64-linux (default) or x86_64-none
	Mode   string   `json:"mode,omitempty"`   // hosted, freestanding or kernel; default from the target
	Tags   []string `json:"tags,omitempty"`   // additional build tags
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   ServerInfo         `json:"serverInfo"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type ServerCapabilities struct {
	PositionEncoding           string                   `json:"positionEncoding"`
	TextDocumentSync           TextDocumentSyncOptions  `json:"textDocumentSync"`
	HoverProvider              bool                     `json:"hoverProvider"`
	DefinitionProvider         bool                     `json:"definitionProvider"`
	ReferencesProvider         bool                     `json:"referencesProvider"`
	DocumentSymbolProvider     bool                     `json:"documentSymbolProvider"`
	WorkspaceSymbolProvider    bool                     `json:"workspaceSymbolProvider"`
	DocumentFormattingProvider bool                     `json:"documentFormattingProvider"`
	CompletionProvider         CompletionOptions        `json:"completionProvider"`
	SignatureHelpProvider      SignatureHelpOptions     `json:"signatureHelpProvider"`
	RenameProvider             RenameOptions            `json:"renameProvider"`
	Workspace                  WorkspaceServerCapabilty `json:"workspace"`
}

type TextDocumentSyncOptions struct {
	OpenClose bool        `json:"openClose"`
	Change    int         `json:"change"` // 1: full
	Save      SaveOptions `json:"save"`
}

type SaveOptions struct {
	IncludeText bool `json:"includeText"`
}

type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters"`
}

type SignatureHelpOptions struct {
	TriggerCharacters   []string `json:"triggerCharacters"`
	RetriggerCharacters []string `json:"retriggerCharacters"`
}

type RenameOptions struct {
	PrepareProvider bool `json:"prepareProvider"`
}

type WorkspaceServerCapabilty struct {
	WorkspaceFolders WorkspaceFoldersCapability `json:"workspaceFolders"`
}

type WorkspaceFoldersCapability struct {
	Supported           bool `json:"supported"`
	ChangeNotifications bool `json:"changeNotifications"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// TextDocumentContentChangeEvent is a change with full document sync: the
// whole new text. Range is set only by clients that ignore the sync kind.
type TextDocumentContentChangeEvent struct {
	Range *Range `json:"range,omitempty"`
	Text  string `json:"text"`
}

type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type DidChangeConfigurationParams struct {
	Settings json.RawMessage `json:"settings"`
}

type DidChangeWatchedFilesParams struct {
	Changes []FileEvent `json:"changes"`
}

type FileEvent struct {
	URI  string `json:"uri"`
	Type int    `json:"type"`
}

type DidChangeWorkspaceFoldersParams struct {
	Event WorkspaceFoldersChangeEvent `json:"event"`
}

type WorkspaceFoldersChangeEvent struct {
	Added   []WorkspaceFolder `json:"added"`
	Removed []WorkspaceFolder `json:"removed"`
}

// Diagnostics.

const (
	SeverityError       = 1
	SeverityWarning     = 2
	SeverityInformation = 3
	SeverityHint        = 4
)

type Diagnostic struct {
	Range              Range                          `json:"range"`
	Severity           int                            `json:"severity"`
	Code               string                         `json:"code,omitempty"`
	CodeDescription    *CodeDescription               `json:"codeDescription,omitempty"`
	Source             string                         `json:"source"`
	Message            string                         `json:"message"`
	RelatedInformation []DiagnosticRelatedInformation `json:"relatedInformation,omitempty"`
}

type CodeDescription struct {
	Href string `json:"href"`
}

type DiagnosticRelatedInformation struct {
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int         `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Hover.

type MarkupContent struct {
	Kind  string `json:"kind"` // "markdown" or "plaintext"
	Value string `json:"value"`
}

type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// References.

type ReferenceParams struct {
	TextDocumentPositionParams
	Context ReferenceContext `json:"context"`
}

type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
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
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

type SymbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	Location      Location `json:"location"`
	ContainerName string   `json:"containerName,omitempty"`
}

// Formatting.

type DocumentFormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
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
	Label         string         `json:"label"`
	Kind          int            `json:"kind,omitempty"`
	Detail        string         `json:"detail,omitempty"`
	Documentation *MarkupContent `json:"documentation,omitempty"`
	SortText      string         `json:"sortText,omitempty"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// Signature help.

type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature"`
	ActiveParameter int                    `json:"activeParameter"`
}

type SignatureInformation struct {
	Label         string                 `json:"label"`
	Documentation *MarkupContent         `json:"documentation,omitempty"`
	Parameters    []ParameterInformation `json:"parameters"`
}

type ParameterInformation struct {
	Label [2]int `json:"label"` // UTF-16 offsets into the signature label
}

// Rename.

type RenameParams struct {
	TextDocumentPositionParams
	NewName string `json:"newName"`
}

type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

type PrepareRenameResult struct {
	Range       Range  `json:"range"`
	Placeholder string `json:"placeholder"`
}

type ShowMessageParams struct {
	Type    int    `json:"type"` // 1 error, 2 warning, 3 info, 4 log
	Message string `json:"message"`
}
