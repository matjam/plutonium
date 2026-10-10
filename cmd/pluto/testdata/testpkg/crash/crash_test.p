package crash

import "pluto/testing"

func TestBefore(t *testing.T) {}

func TestPanics(t *testing.T) {
	t.Run("divide", func(t *testing.T) {
		t.Log("dividing by zero")
		defer t.Log("deferred: runs before the panic is reported")
		zero := 0
		t.Logf("%d", Div(1, zero))
	})
}

func TestAfter(t *testing.T) {
	t.Error("never runs")
}
