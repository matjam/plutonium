// Command pkgcov reports statement coverage per package from a cover
// profile produced with -coverpkg, so that code exercised only by the
// end-to-end tests in internal/build counts for the package it lives in.
//
//	go test -count=1 -coverpkg=./internal/... -coverprofile=cover.out ./...
//	go run ./tools/pkgcov cover.out
package main

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

func main() {
	f, _ := os.Open(os.Args[1])
	sc := bufio.NewScanner(f)
	tot := map[string]int{}
	cov := map[string]int{}
	seen := map[string]bool{}
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "mode:") {
			continue
		}
		parts := strings.Fields(line)
		if seen[parts[0]] {
			// merged profiles repeat blocks; count each block once, covered if any run covered it
			if c, _ := strconv.Atoi(parts[2]); c > 0 {
				pkg := path.Dir(strings.Split(parts[0], ":")[0])
				if !seen[parts[0]+"#c"] {
					seen[parts[0]+"#c"] = true
					n, _ := strconv.Atoi(parts[1])
					cov[pkg] += n
				}
			}
			continue
		}
		seen[parts[0]] = true
		pkg := path.Dir(strings.Split(parts[0], ":")[0])
		n, _ := strconv.Atoi(parts[1])
		c, _ := strconv.Atoi(parts[2])
		tot[pkg] += n
		if c > 0 {
			seen[parts[0]+"#c"] = true
			cov[pkg] += n
		}
	}
	var ks []string
	for k := range tot {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("%-60s %5.1f%%\n", k, 100*float64(cov[k])/float64(tot[k]))
	}
}
