//pu:build linux

package debug

import (
	"pluto/alloc"
	"pluto/rt"
	"unsafe"
)

func defaultPages() alloc.PageProvider { return alloc.Pages }

var (
	hooked   bool
	prevExit func(code int) int
)

// register sets up, once, what hosted builds add to every debug allocator:
// faults in its blocks are explained, and at exit the heap is checked and
// leaks are reported.
func register(d *Allocator) {
	if hooked {
		return
	}
	hooked = true
	rt.SetFaultHandler(DescribeFault)
	prevExit = rt.SetExitHook(atExit)
}

// atExit checks every debug allocator's heap and reports its leaks as its
// options say.
func atExit(code int) int {
	failed := false
	for d := all; d != nil; d = d.next {
		d.CheckHeap()
		if d.opts.Leaks != LeaksOff && d.ReportLeaks() > 0 && d.opts.Leaks == LeaksFail {
			failed = true
		}
	}
	if failed && code == 0 {
		print("debug allocator: exiting with status 2 because of leaks\n")
		code = 2
	}
	if prevExit != nil {
		code = prevExit(code)
	}
	return code
}

// EnvOptions returns DefaultOptions changed by the settings in the
// PLUTO_DEBUGALLOC environment variable, a comma-separated list of:
//
//	underflow        guard pages before allocations instead of after
//	canary           canaries instead of guard pages for every allocation
//	canary=N         canaries for allocations smaller than N bytes
//	guards=N         at most N page-guarded blocks (Options.MaxGuarded)
//	quarantine=N     hold N bytes of freed blocks; N may end in K, M or G
//	leaks=off|report|fail
//
// An unknown setting stops the program with a message.
func EnvOptions() Options {
	o := DefaultOptions()
	env := getenv("PLUTO_DEBUGALLOC")
	for len(env) > 0 {
		item := env
		rest := ""
		for i := 0; i < len(env); i++ {
			if env[i] == ',' {
				item = env[:i]
				rest = env[i+1:]
				break
			}
		}
		env = rest
		key, val := item, ""
		for i := 0; i < len(item); i++ {
			if item[i] == '=' {
				key, val = item[:i], item[i+1:]
				break
			}
		}
		ok := true
		switch key {
		case "":
		case "underflow":
			o.Underflow = true
		case "overflow":
			o.Underflow = false
		case "canary":
			if val == "" {
				o.CanaryBelow = ^uintptr(0)
			} else {
				o.CanaryBelow, ok = parseSize(val)
			}
		case "guards":
			var n uintptr
			n, ok = parseSize(val)
			o.MaxGuarded = int(n)
		case "quarantine":
			o.Quarantine, ok = parseSize(val)
		case "leaks":
			switch val {
			case "off":
				o.Leaks = LeaksOff
			case "report":
				o.Leaks = LeaksReport
			case "fail":
				o.Leaks = LeaksFail
			default:
				ok = false
			}
		default:
			ok = false
		}
		if !ok {
			failHead("bad PLUTO_DEBUGALLOC setting \"")
			print(item)
			print("\"\n")
			rt.Abort()
		}
	}
	return o
}

// parseSize parses a decimal number with an optional K, M or G suffix.
func parseSize(s string) (uintptr, bool) {
	if len(s) == 0 {
		return 0, false
	}
	shift := uint(0)
	switch s[len(s)-1] {
	case 'K', 'k':
		shift = 10
	case 'M', 'm':
		shift = 20
	case 'G', 'g':
		shift = 30
	}
	if shift != 0 {
		s = s[:len(s)-1]
		if len(s) == 0 {
			return 0, false
		}
	}
	n := uintptr(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + uintptr(c-'0')
	}
	return n << shift, true
}

// getenv returns the value of environment variable key, or "".
func getenv(key string) string {
	env := rt.Environ()
	for i := 0; ; i++ {
		s := *(**byte)(unsafe.Add(env, i*8))
		if s == nil {
			return ""
		}
		n := 0
		for *(*byte)(unsafe.Add(unsafe.Pointer(s), n)) != 0 {
			n++
		}
		kv := unsafe.String(s, n)
		if len(kv) > len(key) && kv[len(key)] == '=' && kv[:len(key)] == key {
			return kv[len(key)+1:]
		}
	}
}
