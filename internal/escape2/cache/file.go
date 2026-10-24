package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The file of one package (D44: deterministic, versioned, per package):
//
//	file    := magic uvarint(version) string(package path) uvarint(n) entry* checksum
//	entry   := string(function ID) digest(eir) digest(context) digest(reads) string(summary)
//	string  := uvarint(len) bytes
//	digest  := 16 bytes
//	checksum:= the first 32 bytes of SHA-256 of everything before it
//
// Entries are sorted by (function ID, eir, context, reads), unique. A
// function may have several entries: the summaries it has in different
// programs (a library function whose dynamic calls reach other candidates)
// or versions. The summary is appendSummary's encoding. The checksum finds
// accidental damage early; it is not what makes a file safe to use (anyone
// can write a valid one), the driver's check is.
//
// eir is the digest of the function's EIR without positions, with the
// format version, the compiler and the flags (Config) folded in. context is
// the digest of what the function's solve reads that is fixed before its
// SCC is solved: the members of the SCC (ID, shape, EIR), the summaries of
// its callees in earlier SCCs and the candidate set of each dynamic call
// (whose join the solve applies, D47). reads is the digest of the stage B
// facts and contract summaries the solve read (sched.Recorder).

const (
	magic   = "PUESCSUM"
	version = 7 // 2: domain.Key.FieldsGE; 3: summary.Summary.Deferred; 4: domain.Key.Deep; 5: summary.Region.Lits; 6: local arenas (eir/arena.go); 7: Borrowed and Counts keys (eir/borrow.go)
	// maxVariants bounds the entries of one function in a file; past it the
	// entries this run did not write go first.
	maxVariants = 8
)

type digest [16]byte

type entryKey struct {
	id             string
	eir, ctx, read digest
}

type entry struct {
	entryKey
	sum []byte
}

func cmpEntry(a, b entry) int {
	if c := strings.Compare(a.id, b.id); c != 0 {
		return c
	}
	if c := bytes.Compare(a.eir[:], b.eir[:]); c != 0 {
		return c
	}
	if c := bytes.Compare(a.ctx[:], b.ctx[:]); c != 0 {
		return c
	}
	return bytes.Compare(a.read[:], b.read[:])
}

// encodeFile encodes a package's entries, sorted.
func encodeFile(pkg string, entries []entry) []byte {
	slices.SortFunc(entries, cmpEntry)
	entries = slices.CompactFunc(entries, func(a, b entry) bool { return a.entryKey == b.entryKey })
	b := []byte(magic)
	b = binary.AppendUvarint(b, version)
	b = appendString(b, pkg)
	b = binary.AppendUvarint(b, uint64(len(entries)))
	for _, e := range entries {
		b = appendString(b, e.id)
		b = append(b, e.eir[:]...)
		b = append(b, e.ctx[:]...)
		b = append(b, e.read[:]...)
		b = appendString(b, string(e.sum))
	}
	sum := sha256.Sum256(b)
	return append(b, sum[:32]...)
}

func appendString(b []byte, s string) []byte {
	b = binary.AppendUvarint(b, uint64(len(s)))
	return append(b, s...)
}

// decodeFile decodes a package's file. Any damage, a version or package
// that is not this one's, or entries out of order, makes the whole file
// unusable: the caller then has no entries for the package, which costs
// time only.
func decodeFile(pkg string, data []byte) ([]entry, error) {
	if len(data) < len(magic)+32 || string(data[:len(magic)]) != magic {
		return nil, errCorrupt
	}
	body, sum := data[:len(data)-32], data[len(data)-32:]
	if want := sha256.Sum256(body); !bytes.Equal(want[:], sum) {
		return nil, errCorrupt
	}
	r := &reader{b: body[len(magic):]}
	if r.uvarint() != version || r.str() != pkg {
		return nil, errCorrupt
	}
	n := r.count(1 + 3*len(digest{}) + 1)
	entries := make([]entry, 0, n)
	for i := 0; i < n && r.err == nil; i++ {
		var e entry
		e.id = r.str()
		copy(e.eir[:], r.bytesN(len(digest{})))
		copy(e.ctx[:], r.bytesN(len(digest{})))
		copy(e.read[:], r.bytesN(len(digest{})))
		e.sum = r.bytesN(r.count(1))
		if r.err == nil && len(entries) > 0 && cmpEntry(entries[len(entries)-1], e) >= 0 {
			r.fail()
		}
		entries = append(entries, e)
	}
	if r.err == nil && len(r.b) != 0 {
		r.fail()
	}
	if r.err != nil {
		return nil, r.err
	}
	return entries, nil
}

// fileName is the file of a package in dir: a readable part of its path
// and a digest of the whole path, so that two paths never share a file.
func fileName(dir, pkg string) string {
	h := sha256.Sum256([]byte(pkg))
	var b strings.Builder
	for _, c := range pkg {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '.':
			b.WriteRune(c)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 48 {
			break
		}
	}
	return filepath.Join(dir, b.String()+"-"+hex.EncodeToString(h[:6])+".pesc")
}

// writeFile replaces name's contents atomically: a reader sees the old file
// or the new one, never a part (builds run in parallel).
func writeFile(name string, data []byte) error {
	if old, err := os.ReadFile(name); err == nil && bytes.Equal(old, data) {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(name), filepath.Base(name)+".tmp*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), name); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
