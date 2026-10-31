package lower

import (
	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/syntax/ast"
	"compiler/internal/syntax/token"
	"compiler/internal/types2"
	"compiler/pu"
	"pluto/strings"
)

// genMetadataSymbol is the symbol under which a checked freestanding or
// kernel program exports its source of generation metadata memory
// (abi.md section 1.3), func(n uintptr) uintptr. The runtime calls it
// through //pu:extern (gen_pages_none.p), so the source is bound when the
// program is linked, as the panic handler and the scheduler are.
const genMetadataSymbol = "pluto_gen_metadata"

// checkGenMetadata reports a checked freestanding or kernel build that does
// not export the metadata source, or exports it with another signature.
func (l *Lowerer) checkGenMetadata() {
	if !l.cfg.GenCheck || !l.cfg.Freestanding {
		return
	}
	mode := "freestanding"
	if l.cfg.Kernel {
		mode = "kernel"
	}
	text := compat.Concat("A checked ", mode, " build tags heap pointers, and the runtime keeps their generations in metadata it allocates outside the heap. ",
		"With no operating system to map that memory, the program supplies it: a function exported as ", genMetadataSymbol,
		", func(n uintptr) uintptr, that returns the address of n bytes the runtime keeps for good, or 0 when it has none. ",
		"Build with --gencheck=false for a deliberate unchecked build.")
	fix := pu.New(diag.Fix{Intro: "Export a metadata source", Code: "var metadata [4 << 20]byte\nvar metadataUsed uintptr\n\n//pu:export pluto_gen_metadata\nfunc genMetadata(n uintptr) uintptr {\n\tif metadataUsed+n > uintptr(len(metadata)) {\n\t\treturn 0\n\t}\n\tp := uintptr(unsafe.Pointer(&metadata)) + metadataUsed\n\tmetadataUsed += n\n\treturn p\n}"})
	for _, p := range l.prog.Packages {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				for _, dir := range directives(fd.Doc) {
					name, ok := strings.CutPrefix(dir, "export ")
					if !ok || strings.TrimSpace(name) != genMetadataSymbol {
						continue
					}
					fn, _ := p.Info.Defs[fd.Name].(*types2.Func)
					if fn == nil {
						continue
					}
					sig := fn.Type().(*types2.Signature)
					uptr := types2.Typ[types2.Uintptr]
					if sig.Recv() != nil || sig.Params().Len() != 1 || sig.Results().Len() != 1 ||
						!types2.Identical(sig.Params().At(0).Type(), uptr) || !types2.Identical(sig.Results().At(0).Type(), uptr) {
						l.report(fd.Name.Pos(), pu.New(diag.Diagnostic{Code: diag.NoGenMetadata,
							Msg:  compat.Sprintfb("%s, exported as %s, must have the signature func(n uintptr) uintptr", compat.ArgS(fn.Name()), compat.ArgS(genMetadataSymbol)),
							Text: text}))
					}
					return
				}
			}
		}
	}
	l.report(token.NoPos, pu.New(diag.Diagnostic{Code: diag.NoGenMetadata,
		Msg:  compat.Concat(mode, ": generation checks need a metadata source; no function is exported as ", genMetadataSymbol),
		Text: text,
		Fix:  fix}))
}
