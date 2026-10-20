package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// startProfile starts the profiles that compiler development asks for in the
// environment and returns the function that stops them and writes the
// results. Nothing is done when none is set.
//
//	PLUTO_CPUPROFILE=file    a CPU profile (pprof)
//	PLUTO_MEMPROFILE=file    an allocation profile (pprof), written at the end
//	PLUTO_STACKSAMPLE=file   a text profile made by sampling the stacks of all
//	                         goroutines every 10 ms: the functions that were
//	                         running most often, and those on the stack most
//	                         often. For machines where the CPU profiler's timer
//	                         is not available (sandboxes).
//	PLUTO_PROFILE_SECS=n     stop the build after n seconds and write the
//	                         profiles; an interrupt does the same
func startProfile() (stop func()) {
	cpu, mem, samp := os.Getenv("PLUTO_CPUPROFILE"), os.Getenv("PLUTO_MEMPROFILE"), os.Getenv("PLUTO_STACKSAMPLE")
	var stops []func()
	if cpu != "" {
		if f, err := os.Create(cpu); err != nil {
			say(os.Stderr, "pluto: %v\n", err)
		} else if err := pprof.StartCPUProfile(f); err != nil {
			say(os.Stderr, "pluto: %v\n", err)
			f.Close()
		} else {
			stops = append(stops, func() { pprof.StopCPUProfile(); f.Close() })
		}
	}
	if mem != "" {
		stops = append(stops, func() {
			f, err := os.Create(mem)
			if err != nil {
				say(os.Stderr, "pluto: %v\n", err)
				return
			}
			defer f.Close()
			if err := pprof.Lookup("allocs").WriteTo(f, 0); err != nil {
				say(os.Stderr, "pluto: %v\n", err)
			}
		})
	}
	if samp != "" {
		stops = append(stops, sampleStacks(samp))
	}
	run := func() {
		for _, s := range stops {
			s()
		}
	}
	if len(stops) > 0 {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		var after <-chan time.Time
		if secs, err := strconv.Atoi(os.Getenv("PLUTO_PROFILE_SECS")); err == nil && secs > 0 {
			after = time.After(time.Duration(secs) * time.Second)
		}
		go func() {
			select {
			case <-sig:
			case <-after:
			}
			run()
			os.Exit(130)
		}()
	}
	return run
}

// sampleStacks samples the stacks of all goroutines until the returned
// function is called, which writes the counts to file.
func sampleStacks(file string) func() {
	flat, cum, pairs := map[string]int{}, map[string]int{}, map[string]int{}
	samples := 0
	done, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		buf := make([]byte, 1<<20)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
			}
			n := runtime.Stack(buf, true)
			for _, g := range strings.Split(string(buf[:n]), "\n\n") {
				if strings.Contains(g, "sampleStacks") || strings.Contains(g, "[chan receive") || strings.Contains(g, "[select") || strings.Contains(g, "[sleep") || strings.Contains(g, "[syscall") || strings.Contains(g, "[IO wait") || strings.Contains(g, "[semacquire") {
					continue
				}
				seen := map[string]bool{}
				top := true
				first := ""
				for _, ln := range strings.Split(g, "\n")[1:] {
					if strings.HasPrefix(ln, "\t") || ln == "" {
						continue
					}
					if i := strings.LastIndexByte(ln, '('); i > 0 {
						ln = ln[:i]
					}
					if top {
						flat[ln]++
						top = false
						first = ln
					} else if first != "" {
						pairs[first+"  <-  "+ln]++
						first = ""
					}
					if !seen[ln] {
						seen[ln] = true
						cum[ln]++
					}
				}
				samples++
			}
		}
	}()
	return func() {
		close(done)
		<-finished
		f, err := os.Create(file)
		if err != nil {
			say(os.Stderr, "pluto: %v\n", err)
			return
		}
		defer f.Close()
		fmt.Fprintf(f, "%d samples of running goroutines\n", samples)
		for _, m := range []struct {
			name string
			m    map[string]int
		}{{"flat (top frame)", flat}, {"cumulative (anywhere on the stack)", cum}, {"top frame <- its caller", pairs}} {
			type kv struct {
				k string
				v int
			}
			var all []kv
			for k, v := range m.m {
				all = append(all, kv{k, v})
			}
			sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v || all[i].v == all[j].v && all[i].k < all[j].k })
			fmt.Fprintf(f, "\n%s\n", m.name)
			for i, e := range all {
				if i == 40 {
					break
				}
				fmt.Fprintf(f, "%6d %5.1f%%  %s\n", e.v, 100*float64(e.v)/float64(max(samples, 1)), e.k)
			}
		}
	}
}
