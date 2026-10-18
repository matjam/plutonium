package build_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

func TestCountedPathErrors(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main
import (
 "pluto/errors"
 "pluto/os"
 "pluto/syscall"
)
func main() {
 var b [1]byte
 f := os.Open(".") or panic(err)
 _, err := f.Read(b[:])
 var p *os.PathError
 println(errors.As(err, &p), p.Op, p.Path, errors.Is(err, syscall.EISDIR))
 f.Close()
 f = os.Open("/dev/null") or panic(err)
 _, err = f.Write(b[:])
 p = nil
 println(errors.As(err, &p), p.Op, p.Path, errors.Is(err, syscall.EBADF))
 f.Close()
 _, err = f.Read(b[:])
 println(err == os.ErrClosed)
}
`
	for _, level := range []int{0, 2} {
		t.Run(string(rune('0'+level)), func(t *testing.T) {
			testguard.Parallel(t)
			exe := buildDebugAlloc(t, src, level)
			got, status := runDebugAlloc(t, exe, "")
			const want = "true read . true\ntrue write /dev/null true\ntrue\n"
			if status != 0 || got != want {
				t.Fatalf("exit %d, output %q; want %q", status, got, want)
			}
		})
	}
}
