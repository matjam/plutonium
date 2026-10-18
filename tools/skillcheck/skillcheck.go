// Package skillcheck keeps the Plutonium skill (skill/plutonium) honest. It
// extracts the marked code blocks of the skill's Markdown files and checks
// them against the real compiler, so that no example in the skill can drift
// from the language.
//
// A fenced block is checked when its info string starts with one of these
// words:
//
//	pluto-run [exit=N]   a complete program; it must build and run and exit with
//	                     status N (default 0). A fence ```output that follows
//	                     directly gives the exact standard output.
//	pluto-compile [mode=M]
//	                     a complete file that must build (a func main is added to
//	                     package main if it has none; another package is
//	                     imported from a main package the checker writes). With
//	                     mode=kernel or mode=freestanding it is compiled to an
//	                     object file in that build mode and not linked.
//	pluto-error CODE [mode=M]
//	                     a complete file that must fail with the error CODE.
//	pluto-test           a package with _test.p files, run with the test runner
//	                     of pluto test; every file starts with a //file: line.
//	pluto-names          library names, one qualified name per line (strings.Cut,
//	                     vec.Vec.Push, sync/atomic.Int64), that must all exist.
//	pluto-absent         library names that must not exist: Go names that
//	                     Plutonium lacks, which the text warns about.
//
// A line "//file: path" starts a further file of the same example; it is
// not part of the source. Every other fence is illustrative and unchecked.
package skillcheck

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// A Kind says how a block is checked.
type Kind int

const (
	Run Kind = iota
	Compile
	Error
	Test
	Names
	Absent
)

func (k Kind) String() string {
	return [...]string{"pluto-run", "pluto-compile", "pluto-error", "pluto-test", "pluto-names", "pluto-absent"}[k]
}

// A Block is one checked code block.
type Block struct {
	File   string // the Markdown file
	Line   int    // the line of the opening fence
	Kind   Kind
	Code   string // the error code, for Error
	Exit   int    // the expected exit status, for Run
	Mode   string // "", "kernel" or "freestanding", for Compile and Error
	Files  []SourceFile
	Output *string // the expected standard output, for Run, if given
}

// A SourceFile is one file of an example.
type SourceFile struct {
	Name string
	Text string
}

// Name returns the name of the block's subtest.
func (b *Block) Name() string {
	return fmt.Sprintf("%s:%d", strings.ReplaceAll(b.File, "/", "_"), b.Line)
}

var fenceRE = regexp.MustCompile("^(\\s*)(`{3,})\\s*(\\S*)\\s*(.*)$")

// Extract returns the checked blocks of a Markdown file.
func Extract(file, text string) ([]*Block, error) {
	var blocks []*Block
	var cur *Block
	var curLines []string
	var curIndent, curTicks string
	var last *Block // the Run block an output fence may follow
	inOutput := false
	var outLines []string
	var skipFence string // closing fence of an unchecked block
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		m := fenceRE.FindStringSubmatch(line)
		switch {
		case cur != nil:
			if m != nil && m[3] == "" && m[4] == "" && len(m[2]) >= len(curTicks) {
				if err := finish(cur, curLines, curIndent); err != nil {
					return nil, fmt.Errorf("%s:%d: %v", file, cur.Line, err)
				}
				blocks = append(blocks, cur)
				if cur.Kind == Run {
					last = cur
				} else {
					last = nil
				}
				cur, curLines = nil, nil
				continue
			}
			curLines = append(curLines, line)
		case inOutput:
			if m != nil && m[3] == "" && m[4] == "" {
				s := strings.Join(outLines, "\n")
				if len(outLines) > 0 {
					s += "\n"
				}
				last.Output = &s
				inOutput, outLines, last = false, nil, nil
				continue
			}
			outLines = append(outLines, strings.TrimPrefix(line, curIndent))
		case skipFence != "":
			if m != nil && m[3] == "" && m[4] == "" && len(m[2]) >= len(skipFence) {
				skipFence = ""
			}
		case m != nil:
			word := m[3]
			switch {
			case word == "output" && last != nil:
				inOutput, curIndent = true, m[1]
				continue
			case strings.HasPrefix(word, "pluto-"):
				b, err := newBlock(file, n, word, m[4])
				if err != nil {
					return nil, err
				}
				cur, curIndent, curTicks = b, m[1], m[2]
				last = nil
			default:
				skipFence = m[2]
				last = nil
			}
		case strings.TrimSpace(line) != "":
			last = nil
		}
	}
	if cur != nil || inOutput || skipFence != "" {
		return nil, fmt.Errorf("%s: a code fence is not closed", file)
	}
	return blocks, sc.Err()
}

func newBlock(file string, line int, word, rest string) (*Block, error) {
	b := &Block{File: file, Line: line}
	fields := strings.Fields(rest)
	switch word {
	case "pluto-run":
		b.Kind = Run
		for _, f := range fields {
			v, ok := strings.CutPrefix(f, "exit=")
			if !ok {
				return nil, fmt.Errorf("%s:%d: unknown pluto-run option %q", file, line, f)
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: bad exit status %q", file, line, v)
			}
			b.Exit = n
		}
	case "pluto-compile":
		b.Kind = Compile
	case "pluto-test":
		b.Kind = Test
	case "pluto-names":
		b.Kind = Names
	case "pluto-absent":
		b.Kind = Absent
	case "pluto-error":
		b.Kind = Error
		if len(fields) < 1 || !regexp.MustCompile(`^E\d{4}$`).MatchString(fields[0]) {
			return nil, fmt.Errorf("%s:%d: pluto-error needs one error code such as E3301", file, line)
		}
		b.Code = fields[0]
	default:
		return nil, fmt.Errorf("%s:%d: unknown block kind %q", file, line, word)
	}
	if b.Kind == Compile || b.Kind == Error {
		if b.Kind == Error {
			fields = fields[1:]
		}
		for _, f := range fields {
			m, ok := strings.CutPrefix(f, "mode=")
			if !ok || (m != "kernel" && m != "freestanding") {
				return nil, fmt.Errorf("%s:%d: unknown option %q (want mode=kernel or mode=freestanding)", file, line, f)
			}
			b.Mode = m
		}
	} else if len(fields) > 0 && b.Kind != Run {
		return nil, fmt.Errorf("%s:%d: %s takes no options", file, line, word)
	}
	return b, nil
}

// finish splits the lines of a block into its files.
func finish(b *Block, lines []string, indent string) error {
	name := "main.p"
	if b.Kind == Test {
		name = ""
	}
	var cur []string
	flush := func() error {
		if name == "" {
			if len(strings.TrimSpace(strings.Join(cur, ""))) == 0 {
				return nil
			}
			return fmt.Errorf("a pluto-test block must start with a //file: line")
		}
		if strings.TrimSpace(strings.Join(cur, "")) == "" {
			return nil // nothing before the first //file: line
		}
		b.Files = append(b.Files, SourceFile{Name: name, Text: strings.Join(cur, "\n") + "\n"})
		return nil
	}
	for _, l := range lines {
		l = strings.TrimPrefix(l, indent)
		if p, ok := strings.CutPrefix(l, "//file:"); ok {
			if err := flush(); err != nil {
				return err
			}
			name, cur = strings.TrimSpace(p), nil
			continue
		}
		cur = append(cur, l)
	}
	if err := flush(); err != nil {
		return err
	}
	if len(b.Files) == 0 {
		return fmt.Errorf("an empty %s block", b.Kind)
	}
	return nil
}
