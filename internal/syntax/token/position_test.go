package token_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/syntax/token"
)

func TestAddExistingFilesOutOfOrder(t *testing.T) {
	original := token.NewFileSet()
	files := []*token.File{
		original.AddFile("a.p", -1, 10),
		original.AddFile("b.p", -1, 10),
		original.AddFile("c.p", -1, 10),
	}
	set := token.NewFileSet()
	// Inserting the rightmost range before its middle neighbor must
	// preserve lookup and ordered iteration in the augmented file set.
	set.AddExistingFiles(files[0], files[2], files[1])
	for _, file := range files {
		if got := set.File(file.Pos(5)); got != file {
			t.Fatalf("lookup in %s = %v", file.Name(), got)
		}
	}
	i := 0
	set.Iterate(func(file *token.File) bool {
		if file != files[i] {
			t.Fatalf("file %d = %s, want %s", i, file.Name(), files[i].Name())
		}
		i++
		return true
	})
	if i != len(files) {
		t.Fatalf("iterated %d files, want %d", i, len(files))
	}
}
