package lower_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/lower"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/types2"
)

// TestLowerTranslatedCompiler lowers the translated front end (the demo
// selfhost/demo/lowerdemo imports the loader, the checks, the lowerer and the
// writer of SSA: about 100,000 lines of Plutonium) after the lifetime checks,
// and reports how long that takes and what it says. It takes minutes and
// several GB, so it runs only when PLUTO_SCALE_TEST is set. The lowerer
// reports what it cannot lower as errors (the checks before it have run),
// which the test prints.
func TestLowerTranslatedCompiler(t *testing.T) {
	if os.Getenv("PLUTO_SCALE_TEST") == "" {
		t.Skip("takes minutes and several GB; set PLUTO_SCALE_TEST=1")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	tags := load.BuildTags(false, false, nil)
	tags["gencheck"] = true
	cfg := &load.Config{Tags: tags, Stdlib: plutonium.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")}
	demo := os.Getenv("PLUTO_SCALE_DEMO")
	if demo == "" {
		demo = "lowerdemo"
	}
	prog, err := load.Load(cfg, filepath.Join(root, "selfhost", "demo", demo))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	start := time.Now()
	info := &moves.Info{}
	if os.Getenv("PLUTO_SCALE_FAST") == "" {
		// Without the move checker's account the lowerer reports what
		// it cannot lower all the same, in a fraction of the time (it
		// destroys owners wrongly, which this test does not look at).
		var err error
		info, err = moves.Analyze(prog)
		if err != nil {
			t.Logf("lifetime errors:\n%v", err)
		}
		t.Logf("escape analysis and moves: %v", time.Since(start).Round(time.Second))
	}
	start = time.Now()
	_, err = lower.Lower(&lower.Config{Sizes: cfg.Sizes, GenCheck: true, Moves: info}, prog)
	t.Logf("lowering: %v", time.Since(start).Round(time.Second))
	if err != nil {
		t.Errorf("lowering reports:\n%v", err)
	}
}
