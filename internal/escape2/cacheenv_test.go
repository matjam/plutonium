package escape2

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestCacheEnv checks that PLUTO_ESCAPE_CACHE makes Check write the D44
// cache and that a second Check, from the files, gives the same errors.
func TestCacheEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLUTO_ESCAPE_CACHE", dir)
	for _, src := range []string{accepted, rejected} {
		prog := loadSrc(t, src)
		first := fmt.Sprint(Check(prog))
		files, _ := filepath.Glob(filepath.Join(dir, "*.pesc"))
		if len(files) == 0 {
			t.Fatal("no cache files written")
		}
		if second := fmt.Sprint(Check(prog)); second != first {
			t.Errorf("from the cache:\n%s\nwant:\n%s", second, first)
		}
		t.Setenv("PLUTO_ESCAPE_CACHE", "")
		if cold := fmt.Sprint(Check(prog)); cold != first {
			t.Errorf("without the cache:\n%s\nwith it:\n%s", cold, first)
		}
		t.Setenv("PLUTO_ESCAPE_CACHE", dir)
	}
}
