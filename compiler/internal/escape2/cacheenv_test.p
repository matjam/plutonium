// Ported from internal/escape2/cacheenv_test.go at the go-cutover tag.
// t.Setenv is compat.SetenvForTest; t.TempDir is testenv.TempDir.

package escape2

import (
	"pluto/path/filepath"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/testenv"
)

// TestCacheEnv checks that PLUTO_ESCAPE_CACHE makes Check write the D44
// cache and that a second Check, from the files, gives the same errors.
func TestCacheEnv(t *testing.T) {
	dir := testenv.TempDir(t)
	defer testenv.Remove(dir)
	restore := compat.SetenvForTest("PLUTO_ESCAPE_CACHE", dir)
	defer restore()
	for _, src := range []string{accepted, rejected} {
		prog := loadSrc(t, src)
		first := compat.Sprint(Check(prog))
		files := testenv.Glob(filepath.Join(dir, "*.pesc"))
		if len(files) == 0 {
			t.Fatal("no cache files written")
		}
		if second := compat.Sprint(Check(prog)); second != first {
			t.Errorf("from the cache:\n%s\nwant:\n%s", second, first)
		}
		compat.SetenvForTest("PLUTO_ESCAPE_CACHE", "")
		if cold := compat.Sprint(Check(prog)); cold != first {
			t.Errorf("without the cache:\n%s\nwith it:\n%s", cold, first)
		}
		compat.SetenvForTest("PLUTO_ESCAPE_CACHE", dir)
	}
}
