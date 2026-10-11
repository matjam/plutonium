package format

import "github.com/matjam/plutonium/internal/syntax/parser"

// The tests format plain Go source, go statements included, and compare it
// with go/format. The formatter itself rejects a go statement (a task is
// started with task, language spec 12.12), so the tests parse in the mode
// that keeps the spelling "go".
func init() { parserMode |= parser.AllowGoStmt }
