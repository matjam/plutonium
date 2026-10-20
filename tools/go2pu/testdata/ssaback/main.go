// Command ssaback is the back end of the compiler as a program of its own:
// it reads an SSA program in the textual form of internal/ssatext (the Go
// front end writes it), optimises it, generates code for every function and
// writes the object file to standard output. go2pu translates it with the
// packages it uses, and TestMixedPipeline (tools/go2pu) requires the object
// the Plutonium build writes to equal, byte for byte, the object the Go
// back end writes for the same program, at -O0 and -O2, over the test corpus.
//
// Usage: ssaback -O 0|2 [-notrace] [-nodwarf] file
//
// Without arguments it compiles a small built-in program and prints the
// object file in hexadecimal, so that TestDifferential covers it like the
// other demos.
package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/matjam/plutonium/internal/backend"
	"github.com/matjam/plutonium/internal/debuginfo"
	"github.com/matjam/plutonium/internal/opt"
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/ssatext"
	"github.com/matjam/plutonium/internal/syntax/token"
)

func main() {
	level := 0
	cfg := backend.Config{}
	file := ""
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-O":
			i++
			if i >= len(args) || len(args[i]) != 1 || args[i][0] < '0' || args[i][0] > '2' {
				fail("-O needs 0, 1 or 2")
			}
			level = int(args[i][0] - '0')
		case "-notrace":
			cfg.NoTrace = true
		case "-nodwarf":
			cfg.NoDWARF = true
		default:
			if file != "" {
				fail("one input file")
			}
			file = args[i]
		}
	}
	var p *ssa.Program
	var fset *token.FileSet
	if file == "" && len(args) == 0 {
		level = 2
		p, fset = sample(), token.NewFileSet()
	} else if file == "" {
		fail("usage: ssaback -O level file")
	} else {
		text, err := os.ReadFile(file)
		if err != nil {
			fail(err.Error())
		}
		p, fset, err = ssatext.Read(text)
		if err != nil {
			fail(err.Error())
		}
	}
	if err := opt.ForLevel(level).Run(p, nil); err != nil {
		fail("opt: " + err.Error())
	}
	obj, err := backend.Emit(&cfg, p, debuginfo.New(fset))
	if err != nil {
		fail("emit: " + err.Error())
	}
	var out bytes.Buffer
	if err := obj.Write(&out); err != nil {
		fail("write: " + err.Error())
	}
	if len(args) == 0 {
		fmt.Printf("%x\n", out.Bytes())
		return
	}
	os.Stdout.Write(out.Bytes())
}

// sample is the function that adds 0 to n-1 in a loop.
func sample() *ssa.Program {
	p := ssa.NewProgram()
	f := ssa.NewFunc("sum")
	f.Sym = p.Sym("demo.sum", ssa.SymFunc)
	f.Params = []*ssa.Type{ssa.TI64}
	f.Results = []*ssa.Type{ssa.TI64}
	entry := f.Entry
	mem := entry.NewValue(ssa.OpInitMem, ssa.TMem)
	n := entry.NewValueI(ssa.OpArg, ssa.TI64, 0)
	zero := f.ConstInt(ssa.TI64, 0)
	one := f.ConstInt(ssa.TI64, 1)
	header := f.NewBlock(ssa.BlockIf)
	body := f.NewBlock(ssa.BlockPlain)
	exit := f.NewBlock(ssa.BlockRet)
	entry.AddEdgeTo(header)
	header.AddEdgeTo(body)
	header.AddEdgeTo(exit)
	body.AddEdgeTo(header)
	i := header.NewValue(ssa.OpPhi, ssa.TI64)
	s := header.NewValue(ssa.OpPhi, ssa.TI64)
	header.SetControl(header.NewValue(ssa.OpLess, ssa.TBool, i, n))
	snext := body.NewValue(ssa.OpAdd, ssa.TI64, s, i)
	inext := body.NewValue(ssa.OpAdd, ssa.TI64, i, one)
	i.AddArg(zero)
	i.AddArg(inext)
	s.AddArg(zero)
	s.AddArg(snext)
	exit.SetControl(mem, s)
	p.Funcs = append(p.Funcs, f)
	return p
}

func fail(msg string) {
	fmt.Fprintf(os.Stderr, "ssaback: %s\n", msg)
	os.Exit(1)
}
