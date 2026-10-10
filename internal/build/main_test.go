package build_test

import (
	"flag"
	"os"
	"runtime/debug"
	"testing"
)

// TestMain makes the garbage collector less eager than its default unless
// GOGC or GOMEMLIMIT says otherwise. The tests compile hundreds of programs,
// up to -parallel of them at once, and with GOGC=100 a quarter of their CPU
// time went to collecting. A collection waits for the heap to grow to five
// times what is live, or to a limit that grows with the number of builds
// at once (about what GOGC=100 used at the default -parallel on 24 CPUs),
// whichever comes first.
func TestMain(m *testing.M) {
	flag.Parse()
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(400)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		parallel := 1
		if f := flag.Lookup("test.parallel"); f != nil {
			if g, ok := f.Value.(flag.Getter); ok {
				parallel, _ = g.Get().(int)
			}
		}
		debug.SetMemoryLimit(int64(max(parallel, 4)) * 96 << 20)
	}
	os.Exit(m.Run())
}
