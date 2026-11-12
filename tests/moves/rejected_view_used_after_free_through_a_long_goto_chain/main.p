package main

import "pluto/vec"

// The checks follow the control flow however many jumps it takes: a use
// of a view after the owner frees it, reached through 60 goto hops, is
// an error (language spec sections 18.3, 18.5, 21.4).

func main() {
	var v vec.Vec[int]
	v.Push(7)
	p := v.Ptr(0)
	goto start
L0:
	println(*p)
	return
L1:
	goto L0
L2:
	goto L1
L3:
	goto L2
L4:
	goto L3
L5:
	goto L4
L6:
	goto L5
L7:
	goto L6
L8:
	goto L7
L9:
	goto L8
L10:
	goto L9
L11:
	goto L10
L12:
	goto L11
L13:
	goto L12
L14:
	goto L13
L15:
	goto L14
L16:
	goto L15
L17:
	goto L16
L18:
	goto L17
L19:
	goto L18
L20:
	goto L19
L21:
	goto L20
L22:
	goto L21
L23:
	goto L22
L24:
	goto L23
L25:
	goto L24
L26:
	goto L25
L27:
	goto L26
L28:
	goto L27
L29:
	goto L28
L30:
	goto L29
L31:
	goto L30
L32:
	goto L31
L33:
	goto L32
L34:
	goto L33
L35:
	goto L34
L36:
	goto L35
L37:
	goto L36
L38:
	goto L37
L39:
	goto L38
L40:
	goto L39
L41:
	goto L40
L42:
	goto L41
L43:
	goto L42
L44:
	goto L43
L45:
	goto L44
L46:
	goto L45
L47:
	goto L46
L48:
	goto L47
L49:
	goto L48
L50:
	goto L49
L51:
	goto L50
L52:
	goto L51
L53:
	goto L52
L54:
	goto L53
L55:
	goto L54
L56:
	goto L55
L57:
	goto L56
L58:
	goto L57
L59:
	goto L58
L60:
	goto L59
start:
	v.Free()
	goto L60
}
