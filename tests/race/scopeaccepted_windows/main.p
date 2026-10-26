package main
import "pluto/sync"

func scale(in []int, out []int64, k int) {
	var s sync.Scope
	for i := 0; i < len(in)/k; i++ {
		s.Task(func() {
			row := out[i*k : (i+1)*k]
			for j := range row {
				row[j] = int64(in[i*k+j])
			}
		})
	}
	s.Wait()
	for i := 0; i < len(in); i += 4 {
		s.Task(func() {
			dst := out[i:min(i+4, len(out))]
			for j := range dst {
				dst[j]++
			}
		})
		s.Wait()
	}
}

func main() { scale(nil, nil, 1) }