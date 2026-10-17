package race_test

func init() {
	perCPURejected = append(perCPURejected, struct {
		name, src string
		want      []string
	}{"a pointer carried to the next loop iteration", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	var p *uint64
	for i := 0; i < 2; i++ {
		rt.DisablePreemption()
		if p != nil {
			*p = 1
		}
		p = &ticks
		rt.EnablePreemption()
	}
}`, []string{"main.p:14:3: a pointer to the per-CPU variable ticks is kept in a variable that outlives the loop iteration, outside the region it was taken in", "E2023"}})
	perCPUAccepted = append(perCPUAccepted, struct{ name, src string }{"a pointer taken and used in each loop iteration", `
import "pluto/rt"

//pu:percpu
var ticks uint64

func main() {
	for i := 0; i < 2; i++ {
		rt.DisablePreemption()
		p := &ticks
		*p += 1
		rt.EnablePreemption()
	}
}`})
}
