package build_test

import (
	"bytes"
	"debug/dwarf"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// debugSrc is the program the DWARF tests inspect. The tests refer to its
// line numbers.
const debugSrc = `package main

type big struct {
	a [8]int
}

//pu:noinline
func sum(xs []int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

//pu:noinline
func fill(b *big, n int) int {
	for i := range b.a {
		b.a[i] = n * i
	}
	return sum(b.a[:]) + n
}

//pu:noinline
func wide(n int) int {
	var buf aligned[[8]int, 64]
	for i := 0; i < 8; i++ {
		buf[i] = n + i
	}
	return sum(buf[:]) * 2
}

func main() {
	var b big
	println(fill(&b, 3) + wide(2))
}
`

// buildDebug builds debugSrc and opens the result. It returns the program's
// main.p.
func buildDebug(t *testing.T, cfg build.Config) (*elf.File, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(debugSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Dir = dir
	cfg.Output = filepath.Join(dir, "prog")
	cfg.Stdlib = plutonium.Stdlib()
	if err := build.Build(&cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	f, err := elf.Open(cfg.Output)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, filepath.Join(dir, "main.p")
}

type funcRange struct{ lo, hi uint64 }

// TestDWARF checks the compile units, subprograms, line table and call frame
// information of a program at -O0 and -O2.
func TestDWARF(t *testing.T) {
	testguard.Parallel(t)
	for _, level := range []int{0, 2} {
		t.Run("O"+string(rune('0'+level)), func(t *testing.T) {
			testguard.Parallel(t)
			f, _ := buildDebug(t, build.Config{OptLevel: level})
			// The line table names main.p in directory ".", with no
			// compilation directory, which debug/dwarf joins to main.p.
			const file = "main.p"
			syms, err := f.Symbols()
			if err != nil {
				t.Fatal(err)
			}
			symRange := map[string]funcRange{}
			for _, s := range syms {
				if elf.ST_TYPE(s.Info) == elf.STT_FUNC {
					symRange[s.Name] = funcRange{s.Value, s.Value + s.Size}
				}
			}
			d, err := f.DWARF()
			if err != nil {
				t.Fatal(err)
			}

			// Every subprogram matches its symbol, and main's are in the
			// unit of package main.
			subs := map[string]funcRange{}
			r := d.Reader()
			var mainCU *dwarf.Entry
			var cu *dwarf.Entry
			for {
				e, err := r.Next()
				if err != nil {
					t.Fatal(err)
				}
				if e == nil {
					break
				}
				switch e.Tag {
				case dwarf.TagCompileUnit:
					cu = e
					if e.Val(dwarf.AttrName) == "main" {
						mainCU = e
					}
					if e.Val(dwarf.AttrLanguage) != int64(0x16) {
						t.Errorf("unit %v: language %v, want Go", e.Val(dwarf.AttrName), e.Val(dwarf.AttrLanguage))
					}
					if dir := e.Val(dwarf.AttrCompDir); dir != nil {
						t.Errorf("unit %v: compilation directory %q, want none", e.Val(dwarf.AttrName), dir)
					}
				case dwarf.TagSubprogram:
					name, _ := e.Val(dwarf.AttrName).(string)
					lo, ok := e.Val(dwarf.AttrLowpc).(uint64)
					if !ok {
						if e.Val(dwarf.AttrInline) == nil {
							t.Errorf("subprogram %s has neither code nor DW_AT_inline", name)
						}
						continue // the abstract origin of inlined calls
					}
					hi, _ := e.Val(dwarf.AttrHighpc).(int64)
					subs[name] = funcRange{lo, lo + uint64(hi)}
					if want, ok := symRange[name]; !ok || want != subs[name] {
						t.Errorf("subprogram %s covers %#x-%#x, symbol %#x-%#x", name, lo, lo+uint64(hi), want.lo, want.hi)
					}
					if strings.HasPrefix(name, "main.") && cu != mainCU {
						t.Errorf("subprogram %s outside the main unit", name)
					}
				}
			}
			for _, name := range []string{"main.main", "main.sum", "main.fill", "pluto/rt.panicIndex"} {
				if _, ok := subs[name]; !ok {
					t.Errorf("no subprogram %s", name)
				}
			}
			if mainCU == nil {
				t.Fatal("no compile unit for package main")
			}
			if _, err := d.Ranges(mainCU); err != nil {
				t.Errorf("ranges of the main unit: %v", err)
			}

			// The line table places known statements in their functions.
			lr, err := d.LineReader(mainCU)
			if err != nil || lr == nil {
				t.Fatalf("line reader: %v", err)
			}
			found := map[int]string{}
			var le dwarf.LineEntry
			for lr.Next(&le) == nil {
				if le.File == nil || le.File.Name != file || le.EndSequence {
					continue
				}
				for name, fr := range subs {
					if le.Address >= fr.lo && le.Address < fr.hi && strings.HasPrefix(name, "main.") {
						found[le.Line] = name
					}
				}
			}
			for line, fn := range map[int]string{11: "main.sum", 19: "main.fill", 21: "main.fill", 28: "main.wide", 35: "main.main"} {
				if found[line] != fn {
					t.Errorf("line %d: in %q, want %q (lines found: %v)", line, found[line], fn, found)
				}
			}
			if pc, err := d.LineReader(mainCU); err == nil {
				var e dwarf.LineEntry
				if err := pc.SeekPC(subs["main.sum"].lo, &e); err != nil {
					t.Errorf("no line for the start of main.sum: %v", err)
				} else if e.Line != 8 {
					t.Errorf("main.sum starts at line %d, want 8", e.Line)
				}
			}

			// Each function has an FDE covering it, whose rules start and
			// end at the call boundary.
			fdes := readFDEs(t, f)
			for name, fr := range subs {
				fde, ok := fdes[fr.lo]
				if !ok {
					t.Errorf("%s: no FDE", name)
					continue
				}
				if fde.end != fr.hi {
					t.Errorf("%s: FDE ends at %#x, function at %#x", name, fde.end, fr.hi)
				}
				if rule := fde.ruleAt(fr.lo); rule.reg != 7 || rule.off != 8 {
					t.Errorf("%s: CFA at entry is r%d%+d, want rsp+8", name, rule.reg, rule.off)
				}
			}
			// fill calls sum, so it has a frame: at the call the CFA is
			// further from RSP than the return address alone, and the rule
			// is back to rsp+8 at its return.
			fill := subs["main.fill"]
			call := findCall(t, f, fill, subs["main.sum"].lo)
			if rule := fdes[fill.lo].ruleAt(call); rule.reg != 7 || rule.off <= 8 || rule.off%8 != 0 {
				t.Errorf("main.fill: CFA at the call to sum is r%d%+d", rule.reg, rule.off)
			}
			returns := false
			for _, row := range fdes[fill.lo].rows {
				returns = returns || row.addr > call && row.reg == 7 && row.off == 8
			}
			if !returns {
				t.Errorf("main.fill: the CFA never returns to rsp+8 after the call: %+v", fdes[fill.lo].rows)
			}
			// wide realigns its stack for the 64-byte local, so it keeps
			// RBP as a frame pointer and the CFA follows RBP in its body.
			wide := subs["main.wide"]
			call = findCall(t, f, wide, subs["main.sum"].lo)
			if rule := fdes[wide.lo].ruleAt(call); rule.reg != 6 || rule.off != 16 {
				t.Errorf("main.wide: CFA at the call to sum is r%d%+d, want rbp+16", rule.reg, rule.off)
			}
		})
	}
}

// TestDWARFInlined checks the inlined_subroutine entries of programs whose
// calls are inlined at -O2. In index.p main calls level1 twice (lines 17
// and 18), which calls level2 (line 12), which calls level3 (line 8). In
// faultchain.p main is itself inlined into pluto.main, whose call has no
// position, so call line and column 0.
func TestDWARFInlined(t *testing.T) {
	testguard.Parallel(t)
	calls := "main.level1:11@17:10(main.level2:7@12:9(main.level3:3@8:9)) main.level1:11@18:10(main.level2:7@12:9(main.level3:3@8:9))"
	for _, tc := range []struct{ prog, fn, want string }{
		{"index.p", "main.main", calls},
		{"faultchain.p", "pluto.main", "main.main:24@0:0(main.walk:15@27:10(main.value:11@18:10))"},
	} {
		if got := inlinedCalls(t, tc.prog)[tc.fn]; got != tc.want {
			t.Errorf("%s: %s's inlined calls:\n%s\nwant\n%s", tc.prog, tc.fn, got, tc.want)
		}
	}
}

// inlinedCalls builds testdata/trace/prog at -O2 and describes the calls
// inlined into each function, by function, as "origin:declaration
// line@call line:call column(children)", leaving out calls of functions
// outside package main.
func inlinedCalls(t *testing.T, prog string) map[string]string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "testdata", "trace", prog))
	if err != nil {
		t.Fatal(err)
	}
	dir, _, _ := buildFiles(t, map[string][]byte{"main.p": src}, build.Config{OptLevel: 2})
	f, err := elf.Open(filepath.Join(dir, "prog"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	d, err := f.DWARF()
	if err != nil {
		t.Fatal(err)
	}
	r := d.Reader()
	var describe func() string
	describe = func() string {
		var parts []string
		for {
			e, err := r.Next()
			if err != nil {
				t.Fatal(err)
			}
			if e == nil || e.Tag == 0 {
				return strings.Join(parts, " ")
			}
			if e.Tag != dwarf.TagInlinedSubroutine {
				t.Fatalf("unexpected %v in a subprogram", e.Tag)
			}
			off, _ := e.Val(dwarf.AttrAbstractOrigin).(dwarf.Offset)
			or := d.Reader()
			or.Seek(off)
			origin, err := or.Next()
			if err != nil {
				t.Fatal(err)
			}
			rs, err := d.Ranges(e)
			if err != nil || len(rs) == 0 {
				t.Errorf("inlined %v: ranges %v, %v", origin.Val(dwarf.AttrName), rs, err)
			}
			name, _ := origin.Val(dwarf.AttrName).(string)
			if origin.Val(dwarf.AttrInline) == nil {
				t.Errorf("inlined %s: origin without DW_AT_inline", name)
			}
			// gdb leaves an inlined call without a call file out of
			// backtraces, so every call has one, even with no line.
			if e.Val(dwarf.AttrCallFile) == nil {
				t.Errorf("inlined %s: no DW_AT_call_file", name)
			}
			s := fmt.Sprintf("%v:%v@%v:%v", name, origin.Val(dwarf.AttrDeclLine), e.Val(dwarf.AttrCallLine), e.Val(dwarf.AttrCallColumn))
			if e.Children {
				s += "(" + describe() + ")"
			}
			if strings.HasPrefix(name, "main.") { // not println's runtime calls
				parts = append(parts, s)
			}
		}
	}
	got := map[string]string{}
	for {
		e, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if e == nil {
			break
		}
		if name, _ := e.Val(dwarf.AttrName).(string); e.Tag == dwarf.TagSubprogram && e.Children {
			got[name] = describe()
		}
	}
	return got
}

// dwarfFiles returns the compilation directory of every unit and the file
// names of every line table. debug/dwarf joins relative names to their
// directories with path.Join, so "./main.p" in "." reads as "main.p".
func dwarfFiles(t *testing.T, f *elf.File) (dirs, files []string) {
	t.Helper()
	d, err := f.DWARF()
	if err != nil {
		t.Fatal(err)
	}
	r := d.Reader()
	for {
		e, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if e == nil {
			break
		}
		if e.Tag != dwarf.TagCompileUnit {
			r.SkipChildren()
			continue
		}
		dir, _ := e.Val(dwarf.AttrCompDir).(string)
		dirs = append(dirs, dir)
		lr, err := d.LineReader(e)
		if err != nil || lr == nil {
			continue
		}
		for _, file := range lr.Files() {
			if file != nil {
				files = append(files, file.Name)
			}
		}
	}
	return dirs, files
}

// TestDWARFFileNames checks how DWARF names source files: by default
// relative to the module root (here the package directory, with no
// pluto.mod) and the standard library, with no compilation directory;
// with --trimpath=false by absolute path, with standard library files
// under Config.StdlibDir rather than $PLUTOROOT when it is set.
func TestDWARFFileNames(t *testing.T) {
	testguard.Parallel(t)
	f, _ := buildDebug(t, build.Config{StdlibDir: "/opt/pluto/lib"})
	dirs, files := dwarfFiles(t, f)
	for _, dir := range dirs {
		if dir != "" {
			t.Errorf("compilation directory %q, want none", dir)
		}
	}
	for _, name := range files {
		if filepath.IsAbs(name) || strings.Contains(name, "$PLUTOROOT") {
			t.Errorf("trimmed build names %s", name)
		}
	}
	if !slices.Contains(files, "main.p") || !slices.Contains(files, "pluto/rt/rt.p") {
		t.Errorf("line tables name neither main.p nor pluto/rt/rt.p: %v", files)
	}

	f, file := buildDebug(t, build.Config{StdlibDir: "/opt/pluto/lib", NoTrimPath: true})
	dirs, files = dwarfFiles(t, f)
	for _, dir := range dirs {
		if dir != filepath.Dir(file) {
			t.Errorf("--trimpath=false: compilation directory %q, want %q", dir, filepath.Dir(file))
		}
	}
	for _, name := range files {
		if !filepath.IsAbs(name) {
			t.Errorf("--trimpath=false: file %s is not absolute", name)
		}
	}
	if !slices.Contains(files, file) || !slices.Contains(files, "/opt/pluto/lib/pluto/rt/rt.p") {
		t.Errorf("--trimpath=false: line tables name neither %s nor /opt/pluto/lib/pluto/rt/rt.p: %v", file, files)
	}
}

// TestNoDWARF checks -g=false and -s: no DWARF sections, and with -s no
// symbol table either.
func TestNoDWARF(t *testing.T) {
	testguard.Parallel(t)
	f, _ := buildDebug(t, build.Config{OptLevel: 2, NoDWARF: true})
	for _, s := range f.Sections {
		if strings.HasPrefix(s.Name, ".debug_") {
			t.Errorf("-g=false: section %s present", s.Name)
		}
	}
	if f.Section(".symtab") == nil {
		t.Errorf("-g=false: no symbol table")
	}
	stripped, _ := buildDebug(t, build.Config{OptLevel: 2, Strip: true})
	for _, s := range stripped.Sections {
		if strings.HasPrefix(s.Name, ".debug_") || s.Name == ".symtab" {
			t.Errorf("-s: section %s present", s.Name)
		}
	}
}

// TestGDB sets breakpoints by file and line and by function in programs
// built at -O0 and -O2, and checks the backtraces gdb prints there, through
// a frame without a frame pointer (fill) and one with (wide), and that gdb
// finds the source: run from the module root for a trimmed build, from
// anywhere for --trimpath=false. It is skipped without gdb.
func TestGDB(t *testing.T) {
	testguard.Parallel(t)
	gdb, err := exec.LookPath("gdb")
	if err != nil {
		t.Skip("gdb not installed")
	}
	for _, tc := range []struct {
		level  int
		noTrim bool
	}{{0, false}, {2, false}, {0, true}} {
		_, file := buildDebug(t, build.Config{OptLevel: tc.level, NoTrimPath: tc.noTrim})
		dir := filepath.Dir(file)
		cmd := exec.Command(gdb, "-nx", "-batch",
			"-iex", "set debuginfod enabled off",
			"-ex", "break main.p:19",
			"-ex", "run",
			"-ex", "bt",
			"-ex", "delete",
			"-ex", "break main.sum",
			"-ex", "continue",
			"-ex", "bt",
			"-ex", "continue",
			"-ex", "bt",
			filepath.Join(dir, "prog"))
		name := "./main.p"
		if tc.noTrim {
			name = file // and gdb runs in the test's directory
		} else {
			cmd.Dir = dir
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("O%d trimpath=%v: gdb: %v\n%s", tc.level, !tc.noTrim, err, out)
		}
		text := string(out)
		for _, want := range []string{
			"main.fill () at " + name + ":19\n19\t\t\tb.a[i] = n * i\n", // with its source line
			"main.sum () at " + name + ":",
			"main.fill () at " + name + ":21",
			"main.wide () at " + name + ":30",
			"main.main () at " + name + ":35",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("O%d trimpath=%v: gdb output lacks %q:\n%s", tc.level, !tc.noTrim, want, text)
			}
		}
		if strings.Contains(text, "Backtrace stopped") || strings.Contains(text, "corrupt stack") ||
			strings.Contains(text, "No such file") {
			t.Errorf("O%d trimpath=%v: gdb could not unwind or find the source:\n%s", tc.level, !tc.noTrim, text)
		}
	}

	// Inlined calls are frames of their own.
	src, err := os.ReadFile(filepath.Join("..", "..", "testdata", "trace", "index.p"))
	if err != nil {
		t.Fatal(err)
	}
	dir, _, _ := buildFiles(t, map[string][]byte{"main.p": src}, build.Config{OptLevel: 2})
	file := "./main.p"
	cmd := exec.Command(gdb, "-nx", "-batch", "-iex", "set debuginfod enabled off",
		"-ex", "break main.p:4", "-ex", "run", "-ex", "bt", filepath.Join(dir, "prog"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gdb: %v\n%s", err, out)
	}
	want := regexp.MustCompile(`#0 +main\.level3 \(\) at ` + regexp.QuoteMeta(file) + `:4\n` +
		`#1 +main\.level2 \(\) at ` + regexp.QuoteMeta(file) + `:8\n` +
		`#2 +main\.level1 \(\) at ` + regexp.QuoteMeta(file) + `:12\n` +
		`#3 +main\.main \(\) at ` + regexp.QuoteMeta(file) + `:17\n`)
	if !want.Match(out) {
		t.Errorf("gdb backtrace through inlined calls:\n%s", out)
	}

	// A breakpoint in a task: the backtrace runs through the function
	// the task statement started and the task statement's own frame, and ends
	// cleanly at the runtime's entry, whose call frame information marks
	// it as the outermost frame.
	src, err = os.ReadFile(filepath.Join("..", "..", "testdata", "trace", "gopanic.p"))
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		dir, _, _ := buildFiles(t, map[string][]byte{"main.p": src}, build.Config{OptLevel: level})
		cmd := exec.Command(gdb, "-nx", "-batch", "-iex", "set debuginfod enabled off",
			"-ex", "break main.p:7", "-ex", "run", "-ex", "bt", filepath.Join(dir, "prog"))
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("O%d: gdb: %v\n%s", level, err, out)
		}
		want := regexp.MustCompile(`#0 +main\.level2 \(\) at \./main\.p:7\n` +
			`#1 +(0x[0-9a-f]+ in )?main\.worker \(\) at \./main\.p:11\n` +
			`#2 +(0x[0-9a-f]+ in )?main\.main\.go1 \(\) at \./main\.p:18\n` +
			`#3 +0x[0-9a-f]+ in pluto/rt\.taskMain \(\)`)
		if !want.Match(out) || strings.Contains(string(out), "Backtrace stopped") || strings.Contains(string(out), "corrupt stack") {
			t.Errorf("O%d: gdb backtrace of a task:\n%s", level, out)
		}
	}

	// A fault in an unrolled loop two inlined calls deep (at -O2) has the
	// same backtrace as at -O0, where gdb stops at main.main. At -O2 main
	// is inlined into pluto.main, which the backtrace continues to.
	src, err = os.ReadFile(filepath.Join("..", "..", "testdata", "trace", "faultchain.p"))
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		dir, _, _ := buildFiles(t, map[string][]byte{"main.p": src}, build.Config{OptLevel: level})
		file := "./main.p"
		cmd := exec.Command(gdb, "-nx", "-batch", "-iex", "set debuginfod enabled off",
			"-ex", "run", "-ex", "bt", filepath.Join(dir, "prog"))
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("O%d: gdb: %v\n%s", level, err, out)
		}
		frame := func(n int, fn string, line int) string {
			return fmt.Sprintf(`#%d +(0x[0-9a-f]+ in )?%s \(\) at %s:%d\n`, n, regexp.QuoteMeta(fn), regexp.QuoteMeta(file), line)
		}
		re := frame(0, "main.value", 12) + frame(1, "main.walk", 18) + frame(2, "main.main", 27)
		if level == 2 {
			re += `#3 +0x[0-9a-f]+ in pluto\.main \(\)\n`
		}
		want := regexp.MustCompile(re)
		if !strings.Contains(string(out), "SIGSEGV") || !want.Match(out) {
			t.Errorf("O%d: gdb backtrace of a fault in inlined calls:\n%s", level, out)
		}
	}
}

// An fde is a decoded frame description entry.
type fde struct {
	start, end uint64
	rows       []cfaRow // the CFA rule from each address on
}

type cfaRow struct {
	addr uint64
	reg  uint64
	off  int64
}

func (f fde) ruleAt(pc uint64) cfaRow {
	r := f.rows[0]
	for _, row := range f.rows {
		if row.addr <= pc {
			r = row
		}
	}
	return r
}

// findCall returns the address of the direct call to target in fn.
func findCall(t *testing.T, f *elf.File, fn funcRange, target uint64) uint64 {
	t.Helper()
	text := f.Section(".text")
	code := make([]byte, fn.hi-fn.lo)
	if _, err := text.ReadAt(code, int64(fn.lo-text.Addr)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i+5 <= len(code); i++ {
		if code[i] != 0xE8 {
			continue
		}
		next := fn.lo + uint64(i) + 5
		if next+uint64(int64(int32(binary.LittleEndian.Uint32(code[i+1:])))) == target {
			return fn.lo + uint64(i)
		}
	}
	t.Fatalf("no call to %#x in %#x-%#x", target, fn.lo, fn.hi)
	return 0
}

// readFDEs decodes .debug_frame, interpreting the CFA instructions the
// compiler emits.
func readFDEs(t *testing.T, f *elf.File) map[uint64]fde {
	t.Helper()
	sec := f.Section(".debug_frame")
	if sec == nil {
		t.Fatal("no .debug_frame")
	}
	data, err := sec.Data()
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	out := map[uint64]fde{}
	uleb := func(b *bytes.Reader) uint64 {
		var v uint64
		for shift := 0; ; shift += 7 {
			c, _ := b.ReadByte()
			v |= uint64(c&0x7f) << shift
			if c < 0x80 {
				return v
			}
		}
	}
	for off := 0; off+4 <= len(data); {
		length := int(le.Uint32(data[off:]))
		entry := data[off+4 : off+4+length]
		off += 4 + length
		if le.Uint32(entry) == 0xffffffff {
			continue // the CIE
		}
		start := le.Uint64(entry[4:])
		size := le.Uint64(entry[12:])
		fd := fde{start: start, end: start + size}
		row := cfaRow{addr: start, reg: 7, off: 8}
		fd.rows = append(fd.rows, row)
		b := bytes.NewReader(entry[20:])
		for b.Len() > 0 {
			op, _ := b.ReadByte()
			switch {
			case op&0xc0 == 0x40:
				row.addr += uint64(op & 0x3f)
			case op&0xc0 == 0x80:
				uleb(b)
				continue
			case op&0xc0 == 0xc0:
				continue
			case op == 0x02:
				c, _ := b.ReadByte()
				row.addr += uint64(c)
			case op == 0x03:
				var v [2]byte
				_, _ = b.Read(v[:])
				row.addr += uint64(le.Uint16(v[:]))
			case op == 0x04:
				var v [4]byte
				_, _ = b.Read(v[:])
				row.addr += uint64(le.Uint32(v[:]))
			case op == 0x0c:
				row.reg = uleb(b)
				row.off = int64(uleb(b))
			case op == 0x0d:
				row.reg = uleb(b)
			case op == 0x0e:
				row.off = int64(uleb(b))
			case op == 0x00:
				continue
			default:
				t.Fatalf("FDE at %#x: unexpected CFA instruction %#x", start, op)
			}
			fd.rows = append(fd.rows, row)
		}
		out[start] = fd
	}
	return out
}
