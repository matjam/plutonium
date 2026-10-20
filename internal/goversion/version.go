// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package goversion provides operations on Go language versions in
// toolchain name syntax: strings like "go1.20", "go1.21.0" and "go1.22rc2".
// It is a fork of go/version and internal/gover, which the type checker uses
// to gate features by version; the fork exists so that the type checker can
// be translated to Plutonium (self-hosting plan, section 8, phase 7). Its
// results are those of go/version (version_test.go compares them).
package goversion

import "strings"

// stripGo converts from a "go1.21-custom" version to a "1.21" version.
// If v does not start with "go", stripGo returns the empty string (a known
// invalid version).
func stripGo(v string) string {
	v, _, _ = strings.Cut(v, "-") // strip -custom suffix.
	if len(v) < 2 || v[:2] != "go" {
		return ""
	}
	return v[2:]
}

// Lang returns the Go language version for version x.
// If x is not a valid version, Lang returns the empty string.
// For example:
//
//	Lang("go1.21rc2") = "go1.21"
//	Lang("go1.21.2") = "go1.21"
//	Lang("go1.21") = "go1.21"
//	Lang("go1") = "go1"
//	Lang("bad") = ""
//	Lang("1.21") = ""
func Lang(x string) string {
	v := langOf(stripGo(x))
	if v == "" {
		return ""
	}
	return "go" + v
}

// Compare returns -1, 0, or +1 depending on whether
// x < y, x == y, or x > y, interpreted as Go versions.
// The versions x and y must begin with a "go" prefix: "go1.21" not "1.21".
// Invalid versions, including the empty string, compare less than
// valid versions and equal to each other.
// The language version "go1.21" compares less than the
// release candidate and eventual releases "go1.21rc1" and "go1.21.0".
func Compare(x, y string) int {
	return compare(stripGo(x), stripGo(y))
}

// IsValid reports whether the version x is valid.
func IsValid(x string) bool {
	return parse(stripGo(x)) != version{}
}

// A version is a parsed Go version: major[.Minor[.Patch]][kind[pre]]
// The numbers are the original decimal strings to avoid integer overflows.
type version struct {
	major string // decimal
	minor string // decimal or ""
	patch string // decimal or ""
	kind  string // "", "alpha", "beta", "rc"
	pre   string // decimal or ""
}

func compare(x, y string) int {
	vx := parse(x)
	vy := parse(y)

	if c := cmpInt(vx.major, vy.major); c != 0 {
		return c
	}
	if c := cmpInt(vx.minor, vy.minor); c != 0 {
		return c
	}
	if c := cmpInt(vx.patch, vy.patch); c != 0 {
		return c
	}
	if c := strings.Compare(vx.kind, vy.kind); c != 0 { // "" < alpha < beta < rc
		return c
	}
	if c := cmpInt(vx.pre, vy.pre); c != 0 {
		return c
	}
	return 0
}

// langOf returns the Go language version of x, which has no "go" prefix.
// For example, langOf("1.2.3") == "1.2".
func langOf(x string) string {
	v := parse(x)
	if v.minor == "" || v.major == "1" && v.minor == "0" {
		return v.major
	}
	return v.major + "." + v.minor
}

// parse parses the Go version string x into a version.
// It returns the zero version if x is malformed.
func parse(x string) version {
	var v version

	// Parse major version.
	var ok bool
	v.major, x, ok = cutInt(x)
	if !ok {
		return version{}
	}
	if x == "" {
		// Interpret "1" as "1.0.0".
		v.minor = "0"
		v.patch = "0"
		return v
	}

	// Parse . before minor version.
	if x[0] != '.' {
		return version{}
	}

	// Parse minor version.
	v.minor, x, ok = cutInt(x[1:])
	if !ok {
		return version{}
	}
	if x == "" {
		// Patch missing is same as "0" for older versions.
		// Starting in Go 1.21, patch missing is different from explicit .0.
		if cmpInt(v.minor, "21") < 0 {
			v.patch = "0"
		}
		return v
	}

	// Parse patch if present.
	if x[0] == '.' {
		v.patch, x, ok = cutInt(x[1:])
		if !ok || x != "" {
			// Prereleases (alpha, beta, rc) of patch releases are not allowed.
			return version{}
		}
		return v
	}

	// Parse prerelease.
	i := 0
	for i < len(x) && (x[i] < '0' || '9' < x[i]) {
		if x[i] < 'a' || 'z' < x[i] {
			return version{}
		}
		i++
	}
	if i == 0 {
		return version{}
	}
	v.kind, x = x[:i], x[i:]
	if x == "" {
		return v
	}
	v.pre, x, ok = cutInt(x)
	if !ok || x != "" {
		return version{}
	}

	return v
}

// cutInt scans the leading decimal number at the start of x to an integer
// and returns that value and the rest of the string.
func cutInt(x string) (n, rest string, ok bool) {
	i := 0
	for i < len(x) && '0' <= x[i] && x[i] <= '9' {
		i++
	}
	if i == 0 || x[0] == '0' && i != 1 { // no digits or unnecessary leading zero
		return "", "", false
	}
	return x[:i], x[i:], true
}

// cmpInt returns the comparison of x and y interpreted as decimal numbers.
func cmpInt(x, y string) int {
	if x == y {
		return 0
	}
	if len(x) < len(y) {
		return -1
	}
	if len(x) > len(y) {
		return +1
	}
	if x < y {
		return -1
	}
	return +1
}
