package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	funcRe  = regexp.MustCompile(`^func (?:\([^)]*?\*?([A-Z]\w*)(?:\[[^\]]*\])?\) )?([A-Z]\w*)`)
	typeRe  = regexp.MustCompile(`^type ([A-Z]\w*)`)
	declRe  = regexp.MustCompile(`^(?:const|var) ([A-Z]\w*)`)
	blockRe = regexp.MustCompile(`^\t([A-Z]\w*)`)
)

// plutoExports lists the exported names of each standard library package
// under root (the directory holding pluto/), keyed by import path. Methods
// appear as Type.Method. It scans declarations line by line, which is
// enough for gofmt-formatted sources; it only decides whether to warn.
func plutoExports(root string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	_ = filepath.WalkDir(filepath.Join(root, "pluto"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".p") || strings.HasSuffix(p, "_test.p") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return nil
		}
		pkg := filepath.ToSlash(rel)
		if out[pkg] == nil {
			out[pkg] = map[string]bool{}
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		addExports(out[pkg], string(data))
		return nil
	})
	return out
}

func addExports(names map[string]bool, src string) {
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		if m := funcRe.FindStringSubmatch(line); m != nil {
			if m[1] != "" {
				names[m[1]+"."+m[2]] = true
			} else {
				names[m[2]] = true
			}
		}
		if m := typeRe.FindStringSubmatch(line); m != nil {
			names[m[1]] = true
		}
		if m := declRe.FindStringSubmatch(line); m != nil {
			names[m[1]] = true
		}
		switch {
		case line == "const (" || line == "var (":
			inBlock = true
		case line == ")":
			inBlock = false
		case inBlock:
			if m := blockRe.FindStringSubmatch(line); m != nil {
				names[m[1]] = true
			}
		}
	}
}
