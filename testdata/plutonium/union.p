package main

import (
	"pluto/fmt"
	"pluto/syscall"
	"unsafe"
)

// Union types: the device state machine of the sum types proposal,
// construction, type switches, assertions, comparison, interfaces, methods,
// generics, enums and unions as payloads, and layout.

type DeviceState union {
	Off
	Probing struct{ attempt int }
	Ready   struct {
		irq  uint8
		base uintptr
	}
	Failed syscall.Errno
}

func (st DeviceState) String() string {
	switch s := st.(type) {
	case Off:
		return "off"
	case Probing:
		if s.attempt == 1 {
			return "probing (first attempt)"
		}
		return "probing"
	case Ready:
		return "ready"
	case Failed:
		return "failed: " + "see errno"
	}
}

type Device struct {
	name   string
	state  DeviceState
	probes int
}

// step advances the state machine one step, as a driver's probe loop would.
func (d *Device) step() {
	switch s := d.state.(type) {
	case Off:
		d.state = DeviceState.Probing{attempt: 1}
	case Probing:
		d.probes++
		switch {
		case d.name == "nic" && s.attempt == 3:
			d.state = DeviceState.Ready{irq: 11, base: 0xFEB0_0000}
		case d.name == "disk" && s.attempt == 2:
			d.state = DeviceState.Failed(syscall.EIO)
		default:
			d.state = DeviceState.Probing{attempt: s.attempt + 1}
		}
	case Ready, Failed:
		// Final states: nothing to do.
	}
}

func report(d *Device) {
	switch s := d.state.(type) {
	case Ready:
		fmt.Printf("%s: ready, irq %d, base %#x, after %d probes\n", d.name, s.irq, s.base, d.probes)
	case Failed:
		fmt.Printf("%s: failed (%s) after %d probes\n", d.name, s.Error(), d.probes)
	default:
		fmt.Printf("%s: %v\n", d.name, s)
	}
}

// A union whose payload is an enum, another union and a float.
type Level enum {
	Low
	High
}

type Reading union {
	Missing
	Temp  float64
	Count int32
	Mode  Level
	State DeviceState
}

func show(r Reading) {
	switch v := r.(type) {
	case Missing:
		fmt.Println("missing")
	case Temp:
		fmt.Println("temp", v)
	case Count:
		fmt.Println("count", v)
	case Mode:
		fmt.Println("mode", v)
	case State:
		fmt.Println("state", v)
	}
}

// A union too large to pass in registers (abi.md section 3).
type Big union {
	Small uint8
	Large [6]int64
}

func grow(b Big) Big {
	switch v := b.(type) {
	case Small:
		var l [6]int64
		for i := range l {
			l[i] = int64(v) * int64(i+1)
		}
		return Big.Large(l)
	case Large:
		v[0] = -v[0]
		return Big.Large(v)
	}
}

// More than 256 variants: the tag takes two bytes.
type Wide union {
	V0, V1, V2, V3, V4, V5, V6, V7, V8, V9, V10, V11, V12, V13, V14, V15, V16, V17, V18, V19, V20, V21, V22, V23, V24 uint8
	V25, V26, V27, V28, V29, V30, V31, V32, V33, V34, V35, V36, V37, V38, V39, V40, V41, V42, V43, V44, V45, V46, V47 uint8
	V48, V49, V50, V51, V52, V53, V54, V55, V56, V57, V58, V59, V60, V61, V62, V63, V64, V65, V66, V67, V68, V69, V70 uint8
	V71, V72, V73, V74, V75, V76, V77, V78, V79, V80, V81, V82, V83, V84, V85, V86, V87, V88, V89, V90, V91, V92, V93 uint8
	V94, V95, V96, V97, V98, V99, V100, V101, V102, V103, V104, V105, V106, V107, V108, V109, V110, V111, V112        uint8
	V113, V114, V115, V116, V117, V118, V119, V120, V121, V122, V123, V124, V125, V126, V127, V128, V129, V130        uint8
	V131, V132, V133, V134, V135, V136, V137, V138, V139, V140, V141, V142, V143, V144, V145, V146, V147, V148        uint8
	V149, V150, V151, V152, V153, V154, V155, V156, V157, V158, V159, V160, V161, V162, V163, V164, V165, V166        uint8
	V167, V168, V169, V170, V171, V172, V173, V174, V175, V176, V177, V178, V179, V180, V181, V182, V183, V184        uint8
	V185, V186, V187, V188, V189, V190, V191, V192, V193, V194, V195, V196, V197, V198, V199, V200, V201, V202        uint8
	V203, V204, V205, V206, V207, V208, V209, V210, V211, V212, V213, V214, V215, V216, V217, V218, V219, V220        uint8
	V221, V222, V223, V224, V225, V226, V227, V228, V229, V230, V231, V232, V233, V234, V235, V236, V237, V238        uint8
	V239, V240, V241, V242, V243, V244, V245, V246, V247, V248, V249, V250, V251, V252, V253, V254, V255, V256        uint8
}

func wide(w Wide) uint8 {
	switch v := w.(type) {
	case V0:
		return v
	case V256:
		return v + 100
	default:
		return 0
	}
}

// Shapes implement an interface through a method on the union.
type Shape union {
	Circle struct{ r int }
	Rect   struct{ w, h int }
}

func (s Shape) Area() int {
	switch v := s.(type) {
	case Circle:
		return 3 * v.r * v.r
	case Rect:
		return v.w * v.h
	}
}

type Areaer interface{ Area() int }

// Package-level unions, with initializers.
var (
	initial = DeviceState.Probing{attempt: 7}
	none    DeviceState
	shapes  = [3]Shape{Shape.Circle{r: 1}, Shape.Rect{w: 2, h: 3}, Shape.Circle{2}}
)

func apply[T, U any](x T, f func(T) U) U { return f(x) }

// A struct holding a union passes and returns in registers, as its words.
type Event struct {
	id int
	st DeviceState
}

func next(e Event) Event {
	e.id++
	e.st = DeviceState.Probing{attempt: e.id}
	return e
}

func main() {
	// The state machine.
	devs := []*Device{{name: "nic"}, {name: "disk"}, {name: "uart"}}
	for range 5 {
		for _, d := range devs {
			d.step()
		}
	}
	for _, d := range devs {
		report(d)
	}
	fmt.Println(initial, none, initial.String())

	// Comparison: the same variant with equal payloads.
	a := DeviceState.Ready{irq: 1, base: 2}
	b := DeviceState.Ready{irq: 1, base: 2}
	c := DeviceState.Ready{irq: 1, base: 3}
	fmt.Println(a == b, a == c, a != DeviceState.Off, none == DeviceState.Off, DeviceState.Failed(syscall.EIO) == DeviceState.Failed(syscall.EIO))

	// Assertions.
	if p, ok := initial.(DeviceState.Probing); ok {
		fmt.Println("asserted attempt", p.attempt)
	}
	_, ok := initial.(DeviceState.Ready)
	fmt.Println("ready?", ok)
	fmt.Println("irq", a.(DeviceState.Ready).irq)

	// Several variants in one case bind the union itself.
	for _, st := range []DeviceState{a, initial, DeviceState.Off} {
		switch s := st.(type) {
		case Ready, Probing:
			fmt.Println("busy:", s)
		case Off, Failed:
			fmt.Println("idle:", s)
		}
	}

	// Payloads of every kind.
	for _, r := range []Reading{Reading.Missing, Reading.Temp(21.5), Reading.Count(-3), Reading.Mode(Level.High), Reading.State(initial)} {
		show(r)
	}

	// Large unions pass by reference.
	g := grow(Big.Small(2))
	g = grow(g)
	if l, ok := g.(Big.Large); ok {
		fmt.Println("large", l[0], l[5])
	}

	// Two-byte tags.
	fmt.Println(wide(Wide.V0(7)), wide(Wide.V256(5)), wide(Wide.V128(9)))
	fmt.Println(Wide.V256(5), Reading.Temp(2.5), Reading.Mode(Level.Low), Reading.Missing, Reading.State(a))

	// Interfaces: a method on the union, and the union in an any.
	var total int
	for _, s := range shapes {
		var ar Areaer = s
		total += ar.Area()
	}
	fmt.Println("total area", total)
	var x any = shapes[1]
	switch v := x.(type) {
	case Shape:
		fmt.Println("an any holding a Shape of area", v.Area())
	}
	fmt.Println(x == any(Shape.Rect{w: 2, h: 3}), x == any(Shape.Rect{w: 3, h: 2}))

	// A variant constructor is a function value.
	mk := Shape.Circle
	fmt.Println(mk(struct{ r int }{5}).Area(), apply(10, Reading.Count).(Reading.Count))

	// Arrays of unions start as the first variant.
	var arr [2]Reading
	show(arr[1])
	arr[0] = Reading.Temp(1.25)
	show(arr[0])

	// A union type declared in a function.
	type Token union {
		EOF
		Num int
		Op  byte
	}
	toks := []Token{Token.Num(1), Token.Op('+'), Token.Num(2), Token.EOF}
	sum := 0
	for _, t := range toks {
		switch v := t.(type) {
		case Num:
			sum += v
		case Op:
			fmt.Printf("op %c\n", v)
		case EOF:
			fmt.Println("sum", sum)
		}
	}

	// Unions in structs and arrays, passed by value and compared.
	ev := next(Event{id: 1})
	fmt.Println(ev.id, ev.st, ev == Event{id: 2, st: DeviceState.Probing{attempt: 2}}, [2]DeviceState{a, c} == [2]DeviceState{a, b})

	fmt.Println(unsafe.Sizeof(DeviceState.Off), unsafe.Alignof(none), unsafe.Sizeof(Wide.V0(0)), unsafe.Sizeof(Big.Small(0)))
}
