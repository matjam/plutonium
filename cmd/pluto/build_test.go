package main

import (
	"strings"
	"testing"
)

// TestBuildModes checks how --mode and --target select the build mode.
func TestBuildModes(t *testing.T) {
	tests := []struct {
		target, mode       string
		freestanding, kern bool
		err                string
	}{
		{target: "x86_64-linux"},
		{target: "x86_64-none", freestanding: true},
		{target: "x86_64-linux", mode: "freestanding", freestanding: true},
		{target: "x86_64-none", mode: "kernel", freestanding: true, kern: true},
		{target: "x86_64-none", mode: "hosted", err: "hosted mode needs an operating system"},
		{target: "x86_64-none", mode: "ring0", err: `unknown mode "ring0"`},
	}
	f := buildCmd.Flags()
	defer func() {
		_ = f.Set("target", "x86_64-linux")
		_ = f.Set("mode", "")
	}()
	for _, tc := range tests {
		if err := f.Set("target", tc.target); err != nil {
			t.Fatal(err)
		}
		if err := f.Set("mode", tc.mode); err != nil {
			t.Fatal(err)
		}
		cfg, err := buildConfig(buildCmd, nil)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s --mode %q: err = %v, want %q", tc.target, tc.mode, err, tc.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s --mode %q: %v", tc.target, tc.mode, err)
			continue
		}
		if cfg.Freestanding != tc.freestanding || cfg.Kernel != tc.kern {
			t.Errorf("%s --mode %q: freestanding %v kernel %v, want %v %v",
				tc.target, tc.mode, cfg.Freestanding, cfg.Kernel, tc.freestanding, tc.kern)
		}
	}
}
