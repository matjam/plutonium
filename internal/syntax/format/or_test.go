package format

import (
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestFormatOr checks that the formatter prints an or statement as it is
// written (language spec 11.27), with one blank around or, and that the
// result is a fixed point.
func TestFormatOr(t *testing.T) {
	testguard.Parallel(t)
	const in = `package p

func f(path string) (int, error) {
	load( path )   or   return
	cfg:=load(path) or return fmt.Errorf( "loading %v: %w",path,err )
	a,b = two( ) or return 0,err
	n := strconv.Atoi(line) or continue // skip this line
	g := os.Open(p) or panic( err )
	cfg := load(path) or {
	log.Printf("loading %v: %v", path, err)
		return err
	}
	cfg = load(path,
		opts) or break
	if v := strconv.Atoi(s) or return;v>0 {
		use(v)
	}
	switch n := strconv.Atoi(line) or continue ; {
	case n > 0:
	}
	or := 3
	f(or)
	return or, nil
}
`
	const want = `package p

func f(path string) (int, error) {
	load(path) or return
	cfg := load(path) or return fmt.Errorf("loading %v: %w", path, err)
	a, b = two() or return 0, err
	n := strconv.Atoi(line) or continue // skip this line
	g := os.Open(p) or panic(err)
	cfg := load(path) or {
		log.Printf("loading %v: %v", path, err)
		return err
	}
	cfg = load(path,
		opts) or break
	if v := strconv.Atoi(s) or return; v > 0 {
		use(v)
	}
	switch n := strconv.Atoi(line) or continue; {
	case n > 0:
	}
	or := 3
	f(or)
	return or, nil
}
`
	got, err := Source([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	again, err := Source(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != want {
		t.Errorf("formatting is not a fixed point:\n%s", again)
	}
}
