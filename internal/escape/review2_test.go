package escape_test

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

var updateReview2 = flag.Bool("update", false, "rewrite testdata/review2/want.txt with the diagnostics the analysis gives")

// TestReview2 checks the programs of the second adversarial review of the
// escape analysis (testdata/review2, one per file; the review is of commits
// 45d516b4, bac17283 and 0bb4fdd9). Each keeps, returns or stores a pointer into
// a frame that is gone, by a way the analysis once missed or that the review
// tried and found reported: each must stay reported, at the line where the
// program does it. want.txt holds the first diagnostic of each program; run
// the test with -update to rewrite it, and read the diff.
func TestReview2(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "review2", "*.p"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no programs: %v", err)
	}
	wantFile := filepath.Join("testdata", "review2", "want.txt")
	want := map[string]string{}
	if !*updateReview2 {
		data, err := os.ReadFile(wantFile)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			name, first, ok := strings.Cut(line, ": ")
			if !ok {
				t.Fatalf("want.txt: bad line %q", line)
			}
			want[name] = first
		}
	}
	var mu sync.Mutex
	got := map[string]string{}
	t.Run("programs", func(t *testing.T) {
		for _, f := range files {
			name := strings.TrimSuffix(filepath.Base(f), ".p")
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				src, err := os.ReadFile(f)
				if err != nil {
					t.Fatal(err)
				}
				err = check(t, string(src))
				if err == nil {
					t.Fatal("accepted")
				}
				first, _, _ := strings.Cut(fullText(err), "\n")
				mu.Lock()
				got[name] = first
				mu.Unlock()
				if !*updateReview2 && first != want[name] {
					t.Errorf("first diagnostic\n got: %s\nwant: %s", first, want[name])
				}
			})
		}
	})
	if !*updateReview2 {
		if len(want) != len(files) {
			t.Errorf("want.txt has %d programs, testdata/review2 %d", len(want), len(files))
		}
		return
	}
	var names []string
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name + ": " + got[name] + "\n")
	}
	if err := os.WriteFile(wantFile, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
