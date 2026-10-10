package calc

import "pluto/testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Errorf("Add(2, 3) = %d, want 5", got)
	}
	t.Log("added", 2, "and", 3)
}

func TestWrong(t *testing.T) {
	t.Logf("checking %s", "Add")
	if got := Add(2, 2); got != 5 {
		t.Errorf("Add(2, 2) = %d, want 5", got)
	}
}

func TestSub(t *testing.T) {
	t.Run("small numbers", func(t *testing.T) {
		if Add(1, 1) != 2 {
			t.Fatal("1+1 != 2")
		}
	})
	t.Run("fatal", func(t *testing.T) {
		t.Fatalf("stop %d", 1)
		t.Log("not reached")
	})
	t.Run("skipped", func(t *testing.T) {
		t.Skip("not today")
	})
}

// TestCleanup stops in a helper with Fatal: the deferred calls of the
// helper and of the test still run, innermost first.
func TestCleanup(t *testing.T) {
	defer t.Log("deferred in the test")
	check(t, 2)
	t.Log("not reached")
}

func check(t *testing.T, n int) {
	defer t.Logf("deferred in check(%d)", n)
	if Add(n, n) != 5 {
		t.Fatalf("%d+%d != 5", n, n)
	}
	t.Log("not reached either")
}

func BenchmarkAdd(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	s := 0
	for i := 0; i < b.N; i++ {
		s = Add(s, i)
	}
	if s == -1 {
		b.Fatal("impossible")
	}
}
