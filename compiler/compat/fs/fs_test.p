package fs

import (
	"pluto/errors"
	"pluto/os"
	"pluto/testing"
	"compiler/compat"
)

func TestDirFS(t *testing.T) {
	root := os.TempDirLossy()
	dir := root + "/compatfs_test"
	_ = os.RemoveAll(dir)
	os.MkdirAll(dir+"/sub", 0o755) or {
		t.Fatalf("MkdirAll: %v", err)
		return
	}
	os.WriteFile(dir+"/b.txt", compat.Bytes("bee"), 0o644) or {
		t.Fatalf("WriteFile: %v", err)
		return
	}
	os.WriteFile(dir+"/a.p", compat.Bytes("package a\n"), 0o644) or {
		t.Fatalf("WriteFile: %v", err)
		return
	}

	fsys := DirFS(dir)
	ents := ReadDir(fsys, ".") or {
		t.Fatalf("ReadDir: %v", err)
		return
	}
	if len(ents) != 3 || ents[0].Name() != "a.p" || ents[1].Name() != "b.txt" || ents[2].Name() != "sub" {
		t.Fatalf("ReadDir: %d entries", len(ents))
	}
	if ents[0].IsDir() || !ents[2].IsDir() || ents[0].Type().IsDir() || !ents[2].Type().IsDir() {
		t.Fatalf("IsDir or Type")
	}
	info := ents[1].Info() or {
		t.Fatalf("Info: %v", err)
		return
	}
	if info.Name() != "b.txt" || info.Size() != 3 || info.IsDir() || !info.Mode().IsRegular() {
		t.Fatalf("Info: %s %d", info.Name(), info.Size())
	}
	data := ReadFile(fsys, "b.txt") or {
		t.Fatalf("ReadFile: %v", err)
		return
	}
	if compat.String(data) != "bee" {
		t.Fatalf("ReadFile: %q", data)
	}
	st := Stat(fsys, "sub") or {
		t.Fatalf("Stat: %v", err)
		return
	}
	if !st.IsDir() || st.Name() != "sub" {
		t.Fatalf("Stat: %s", st.Name())
	}
	sub := ReadDir(fsys, "sub") or {
		t.Fatalf("ReadDir sub: %v", err)
		return
	}
	if len(sub) != 0 {
		t.Fatalf("sub has %d entries", len(sub))
	}
	if _, err := Stat(fsys, "missing"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("Stat missing: %v", err)
	}
	if _, err := ReadFile(fsys, "missing"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("ReadFile missing: %v", err)
	}
	if _, err := ReadDir(fsys, "missing"); !errors.Is(err, ErrNotExist) {
		t.Fatalf("ReadDir missing: %v", err)
	}
	_ = os.RemoveAll(dir)
}
