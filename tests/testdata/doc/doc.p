// Package sample documents source without requiring an executable.
package sample

import "pluto/hash"

// Grouped declarations retain their shared documentation.
const (
	// First has its own documentation.
	First = iota
	Second
	privateConstant = 9
)

var (
	Title  = "sample" // Title is counted text.
	Hidden []byte
)

// Buffer owns its data.
type Buffer owner struct {
	// Data is borrowed from this owner.
	Data    []byte
	private int
}

// Drop releases the buffer.
func (b *Buffer) Drop() {}

// Len accepts a default argument.
func (b *Buffer) Len(extra int = 2) int { return len(b.Data) + extra }

// consume is an owner method.
func (b Buffer) consume() {}

// Strings is a counted aggregate.
type Strings struct {
	Text string
	Ref  shared[int]
}

// Mixed has view precedence over counted fields.
type Mixed struct {
	Strings
	View []byte
}

type CountAlias = Strings
type Pointer *Buffer
type Text string
type Map map[string]int
type Channel chan string
type Shared shared[int]
type Synced synced[int]
type Weak weak[shared[int]]
type Owned owned[int]
type Callback func(string)
type Reader interface {
	// Read gets a value.
	Read() int
}
type Error interface{ Error() string }
type Closeable owner interface{ Close() }
type OwnAlias = Closeable

type BufferAlias = Buffer
type Larger owner struct{ Buffer }
type Foreign = hash.FNV32a
type Generic[T any] struct{ Value T }

func (g *Generic[T]) Get(extra int = 1) T { return g.Value }

type StringGeneric = Generic[string]
type PointerAlias = *Buffer

// Group fallback documents declarations with no specific comment.
type (
	Grouped       int // Grouped has specific trailing documentation.
	GroupFallback int
)

// Color has documented enum members.
type Color enum {
	// Red is the first color.
	Red
	Blue
}

// Choice is a counted union.
type Choice union {
	None
	Some string
}

// Box preserves its owner constraint.
type Box[T owner any] owner struct{ Value T }

// Identity preserves generic parameter names.
func Identity[T any](v T) T { return v }

// Greet preserves source default expressions.
func Greet(name string = "world") string { return name }

// foreign is never linked by documentation.
//
//pu:extern doc_missing_symbol
func foreign()

func init() { panic("documentation must not execute this initializer") }
