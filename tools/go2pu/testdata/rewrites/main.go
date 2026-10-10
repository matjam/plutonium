// Command rewrites exercises every rewrite go2pu makes. TestRewritesDifferential
// translates it and checks that Go and Plutonium print the same text, so
// its output must not depend on map iteration order.
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type node struct {
	name string
	next *node
}

type named interface{ Name() string }

func (n *node) Name() string { return n.name }

type point struct{ x, y int }

type header struct {
	Magic [4]byte
	Count uint16
	Flags uint8
	Pad   uint8
	Size  uint64
	Words [2]uint32
}

type table struct {
	byName map[string]*node
	order  []string
}

var errBase = errors.New("base")

// newNode returns a pointer to a composite literal, which Go moves to the
// heap and Plutonium would leave in the frame.
func newNode(name string) *node { return &node{name: name} }

// pair returns a slice literal, likewise.
func pair(a, b int) []int { return []int{a, b} }

func (t *table) add(n *node) {
	if t.byName == nil {
		t.byName = make(map[string]*node)
	}
	t.byName[n.name] = n
	t.order = append(t.order, n.name)
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func maps() {
	counts := map[string]int{"a": 1, "b": 2}
	counts["c"] = 3
	counts["a"] += 10
	counts["b"]++
	if v, ok := counts["c"]; ok {
		fmt.Println("c is", v)
	}
	var v, ok = counts["zz"]
	fmt.Println("zz", v, ok, len(counts))
	delete(counts, "b")
	for _, k := range sortedKeys(counts) {
		fmt.Println(k, counts[k])
	}
	sum := 0
	for _, n := range counts {
		sum += n
	}
	var k string
	var n int
	for k, n = range counts {
		sum += n - n
	}
	_ = k
	fmt.Println("sum", sum)

	// Labeled loops over a map.
	found := 0
outer:
	for key := range counts {
		for i := 0; i < 3; i++ {
			if key == "a" {
				found++
				continue outer
			}
		}
	}
	fmt.Println("found", found)
	clear(counts)
	fmt.Println("cleared", len(counts))

	var nilMap map[string]int
	fmt.Println("nil map", nilMap["x"], len(nilMap))

	// Pointer and interface keys.
	a, b := newNode("a"), newNode("b")
	seen := map[*node]bool{a: true}
	seen[b] = true
	fmt.Println("seen", seen[a], seen[b], seen[newNode("c")])
	byIface := map[named]int{}
	byIface[a] = 1
	byIface[b] = 2
	fmt.Println("iface", byIface[a], byIface[b], len(byIface))

	// Literals with elided element types.
	pts := map[string]point{"o": {0, 0}, "x": {1, 0}}
	ptrs := map[string]*point{"p": {2, 3}}
	nested := map[string]map[string]bool{"outer": {"inner": true}}
	fmt.Println("lits", pts["x"].x, ptrs["p"].y, nested["outer"]["inner"], len(nested["none"]))

	var t table
	t.add(a)
	t.add(b)
	fmt.Println("table", t.byName["b"].name, len(t.order))
}

func stringsAndBytes() {
	s := "go"
	s += "2"
	t := s + "pu" + "!" + s
	fmt.Println(t, len(t))
	bs := []byte(t)
	bs[0] = 'G'
	fmt.Println(string(bs), string(rune(0x263A)))
	bs = append(bs, " tail"...)
	fmt.Println(string(bs))

	var sb strings.Builder
	for i := 0; i < 3; i++ {
		fmt.Fprintf(&sb, "[%d]", i)
	}
	sb.WriteString("end")
	fmt.Println(sb.String(), sb.Len())
	fmt.Println(strings.Join([]string{"x", "y", "z"}, "-"))

	msg := fmt.Sprintf("%s=%d", "k", 42)
	err := fmt.Errorf("wrapped %q: %w", msg, errBase)
	fmt.Println(err, errors.Is(err, errBase), errors.Unwrap(err) == errBase)
}

func sorting() {
	type item struct {
		key, seq int
	}
	items := []item{{3, 0}, {1, 1}, {2, 2}, {1, 3}, {3, 4}, {2, 5}, {1, 6}, {0, 7}, {2, 8}, {3, 9},
		{1, 10}, {0, 11}, {2, 12}, {1, 13}, {3, 14}, {0, 15}}
	unstable := append([]item(nil), items...)
	sort.Slice(unstable, func(i, j int) bool { return unstable[i].key < unstable[j].key })
	stable := append([]item(nil), items...)
	sort.SliceStable(stable, func(i, j int) bool { return stable[i].key < stable[j].key })
	for _, it := range unstable {
		fmt.Print(it.key, ":", it.seq, " ")
	}
	fmt.Println()
	for _, it := range stable {
		fmt.Print(it.key, ":", it.seq, " ")
	}
	fmt.Println()
}

func encoding() {
	h := header{Magic: [4]byte{'P', 'U', 'O', 'B'}, Count: 0x0102, Flags: 7, Size: 0x1122334455667788, Words: [2]uint32{1, 0xAABBCCDD}}
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, &h)
	var tail [8]byte
	binary.LittleEndian.PutUint32(tail[:], 0xDEADBEEF)
	binary.LittleEndian.PutUint16(tail[4:], 0xCAFE)
	buf.Write(tail[:])
	for _, x := range buf.Bytes() {
		fmt.Printf("%02x", x)
	}
	fmt.Println()
}

type holder struct {
	p *int
	q *point
	r *string
}

// keep takes the addresses of a parameter and of locals, which Go moves to
// the heap; the translation allocates them.
func keep(n int) *holder {
	v := point{n, n}
	var s string
	var w int = 7
	h := &holder{p: &n, q: &v, r: &s}
	n++
	v.x += w
	s = "kept"
	return h
}

func main() {
	h := keep(41)
	fmt.Println(*h.p, h.q.x, h.q.y, *h.r)
	maps()
	stringsAndBytes()
	sorting()
	encoding()
	p := pair(4, 5)
	n := newNode("solo")
	fmt.Println(p[0]+p[1], n.Name())
}
