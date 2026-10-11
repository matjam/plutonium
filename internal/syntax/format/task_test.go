package format

import (
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestFormatTask checks that the formatter prints a task statement with the
// word task, and leaves a variable named task alone.
func TestFormatTask(t *testing.T) {
	testguard.Parallel(t)
	const in = `package p

func f(q *queue) {
	task   worker( jobs )
	task func( ){
		work( )
	}( )
	task := q.Pop( )
	task.Run( )
	task   =   next
	task[ i ]=v
	go   legacy( )
}
`
	const want = `package p

func f(q *queue) {
	task worker(jobs)
	task func() {
		work()
	}()
	task := q.Pop()
	task.Run()
	task = next
	task[i] = v
	go legacy()
}
`
	// The tests parse go statements (see export_test.go); the formatter
	// keeps their spelling, and pluto itself rejects them.
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
