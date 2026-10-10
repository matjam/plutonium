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
