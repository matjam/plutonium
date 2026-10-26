package main
import (
	"pluto/bytes"
	"pluto/hash"
	"pluto/os"
	"pluto/strings"
	"pluto/vec"
)

func useVec(v vec.Vec[int])           { println(v.Len()) }
func useMap(m hash.Map[string, int])  { println(m.Len()) }
func useBuilder(b strings.Builder)    { println(b.Len()) }
func useBuffer(b bytes.Buffer)        { println(b.Len()) }
func useFile(f *os.File, done chan bool) { println(f.NameLossy()); done <- true }

func main() {
	v := vec.New[int]()
	v.Push(1)
	task useVec(v)
	m := hash.NewMap[string, int](4)
	m.Set("a", 1)
	task useMap(m)
	var b strings.Builder
	b.WriteString("x")
	task useBuilder(b)
	var buf bytes.Buffer
	buf.WriteString("y")
	task useBuffer(buf)
	done := make(chan bool)
	task useFile(os.Stdout, done)
	<-done
}