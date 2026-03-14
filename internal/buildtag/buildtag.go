package buildtag

import (
	"path/filepath"
	"strings"
)

// Eval evaluates a build tag constraint expression against a set of active tags.
// Supports: tag, !tag, tag && tag, tag || tag, (expr)
// The special tag "ignore" always evaluates to false.
func Eval(expr string, tags map[string]bool) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true, nil
	}
	p := &tagParser{input: expr, tags: tags}
	result := p.parseOr()
	return result, nil
}

type tagParser struct {
	input string
	pos   int
	tags  map[string]bool
}

func (p *tagParser) skipSpace() {
	for p.pos < len(p.input) && (p.input[p.pos] == ' ' || p.input[p.pos] == '\t') {
		p.pos++
	}
}

func (p *tagParser) parseOr() bool {
	left := p.parseAnd()
	for {
		p.skipSpace()
		if strings.HasPrefix(p.input[p.pos:], "||") {
			p.pos += 2
			right := p.parseAnd()
			left = left || right
		} else {
			break
		}
	}
	return left
}

func (p *tagParser) parseAnd() bool {
	left := p.parseUnary()
	for {
		p.skipSpace()
		if strings.HasPrefix(p.input[p.pos:], "&&") {
			p.pos += 2
			right := p.parseUnary()
			left = left && right
		} else {
			break
		}
	}
	return left
}

func (p *tagParser) parseUnary() bool {
	p.skipSpace()
	if p.pos < len(p.input) && p.input[p.pos] == '!' {
		p.pos++
		return !p.parsePrimary()
	}
	return p.parsePrimary()
}

func (p *tagParser) parsePrimary() bool {
	p.skipSpace()
	if p.pos < len(p.input) && p.input[p.pos] == '(' {
		p.pos++ // consume '('
		result := p.parseOr()
		p.skipSpace()
		if p.pos < len(p.input) && p.input[p.pos] == ')' {
			p.pos++ // consume ')'
		}
		return result
	}
	// read tag name
	start := p.pos
	for p.pos < len(p.input) {
		c := p.input[p.pos]
		if c == ' ' || c == '\t' || c == '&' || c == '|' || c == ')' {
			break
		}
		p.pos++
	}
	tag := p.input[start:p.pos]
	if tag == "ignore" {
		return false
	}
	return p.tags[tag]
}

// FileMatches returns true if a file should be included in a build with the given tags.
// It checks:
// 1. File name suffix conventions (_amd64.pu, _amd64.s, _linux.pu, etc.)
// 2. //pu:build directives (passed as the directive text after "pu:", e.g. "build amd64")
//
// directives is a list of directive texts from the file (e.g. ["build amd64", "build linux"]).
// ALL directives must match (AND semantics between multiple //pu:build lines).
func FileMatches(filename string, directives []string, tags map[string]bool) (bool, error) {
	base := filepath.Base(filename)
	// strip extension
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	// Check file name suffix: foo_amd64.pu, foo_amd64_linux.pu
	// Split by "_" and check last 1-2 parts as tags
	parts := strings.Split(name, "_")
	if len(parts) >= 2 {
		// Check last part as GOOS or GOARCH
		last := parts[len(parts)-1]
		if isKnownTag(last) {
			if !tags[last] {
				return false, nil
			}
		}
		// Check second-to-last as well (e.g. foo_amd64_linux)
		if len(parts) >= 3 {
			secondLast := parts[len(parts)-2]
			if isKnownTag(secondLast) {
				if !tags[secondLast] {
					return false, nil
				}
			}
		}
	}

	// Check //pu:build directives
	for _, d := range directives {
		d = strings.TrimSpace(d)
		if !strings.HasPrefix(d, "build ") {
			continue
		}
		expr := strings.TrimPrefix(d, "build ")
		ok, err := Eval(expr, tags)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}

	return true, nil
}

// knownTags are recognized OS and architecture tags for filename suffix matching.
var knownTagSet = map[string]bool{
	"amd64": true, "arm64": true, "386": true, "arm": true,
	"riscv64": true, "mips64": true,
	"linux": true, "darwin": true, "windows": true, "freebsd": true,
	"freestanding": true,
}

func isKnownTag(s string) bool {
	return knownTagSet[s]
}
