// Translated by go2pu from the constants of debug/dwarf at the go-cutover tag; edited by hand since.

// Package dwarf holds the names of Go's debug/dwarf that the compiler's packages use,
// with the values Go gives them (tools/go2pu, mirror.go).
package dwarf

type Attr uint32

type Tag uint32

const (
	AttrAbstractOrigin   Attr = 49
	AttrCallColumn       Attr = 87
	AttrCallFile         Attr = 88
	AttrCallLine         Attr = 89
	AttrCompDir          Attr = 27
	AttrDeclFile         Attr = 58
	AttrDeclLine         Attr = 59
	AttrExternal         Attr = 63
	AttrFrameBase        Attr = 64
	AttrHighpc           Attr = 18
	AttrInline           Attr = 32
	AttrLanguage         Attr = 19
	AttrLowpc            Attr = 17
	AttrName             Attr = 3
	AttrProducer         Attr = 37
	AttrRanges           Attr = 85
	AttrStmtList         Attr = 16
	TagCompileUnit       Tag  = 17
	TagInlinedSubroutine Tag  = 29
	TagSubprogram        Tag  = 46
)
