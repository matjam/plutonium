//pu:build amd64 && !freestanding
// <-- punctuation.definition.comment
//^^^^^^^^ keyword.control.directive
//         ^^^^^ entity.name.tag.build-constraint
//               ^^ keyword.operator.logical
//                  ^ keyword.operator.logical

// Package sample exercises the Plutonium grammar; grammar-check.js checks the
// assertion comments. It need not compile. Indentation is spaces so that an
// assertion's carets can sit under an indented token.
package sample
// <------- keyword.package
//      ^^^^^^ entity.name.namespace

import (
    "pluto/alloc"
//  ^ punctuation.definition.string.begin
//   ^^^^^^^^^^^ entity.name.import string.quoted.double
    f "pluto/fmt"
//  ^ entity.alias.import
)

import "unsafe"
// <------ keyword.import
//     ^^^^^^^^ string.quoted.double

/* a block
   comment */
// <-- comment.block

//pu:extern lua_pushinteger
//^^^^^^^^^ keyword.control.directive
//          ^^^^^^^^^^^^^^^ string.unquoted.directive-argument
func pushInteger(L *State, n int64)
// <---- keyword.function
//   ^^^^^^^^^^^ entity.name.function
//               ^ variable.parameter
//                  ^^^^^ entity.name.type !variable.parameter
//                         ^ variable.parameter
//                           ^^^^^ support.type.builtin

// pu:inline is an ordinary comment, not a directive.
//  ^^^^^^^^^ comment.line.double-slash !keyword.control.directive

type Point struct {
// <---- keyword.type
//   ^^^^^ entity.name.type
//         ^^^^^^ keyword.struct
    x, y int
//  ^ variable.other.field
//     ^ variable.other.field
//       ^^^ support.type.builtin
    name string `json:"name"`
//              ^ string.quoted.raw
    io.Reader
//     ^^^^^^ entity.name.type
}

type File owner struct {
//        ^^^^^ keyword.owner
    fd   int32
    open bool
}

type Vec[T owner any] owner struct {
//       ^ entity.name.type.parameter
//         ^^^^^ keyword.owner
//               ^^^ support.type.builtin
//                    ^^^^^ keyword.owner
    data owned[T]
//       ^^^^^ support.type.builtin.constructor
    len  int
//  ^^^ variable.other.field !support.function
}

type ControlReg packed le uint32 {
//              ^^^^^^ keyword.packed
//                     ^^ keyword.byte-order
    mode     bits[uint, 4]
//  ^^^^ variable.other.field
//           ^^^^ keyword.bits
    flags    bits { Enable Busy Ready }
//           ^^^^ keyword.bits
//                  ^^^^^^ variable.other.field.flag
    _        bits[uint, 25]
}

type IPv4Header packed be [20]byte {
//                     ^^ keyword.byte-order
    ihl uint8
}

type Errno enum uint16 {
//         ^^^^ keyword.enum
//              ^^^^^^ support.type.builtin
    OK     = 0
//  ^^ variable.other.enummember
    EPERM
//  ^^^^^ variable.other.enummember
    EIO    = 5
}

type DeviceState union {
//               ^^^^^ keyword.union
    Off
//  ^^^ entity.name.type.variant
    Ready struct{ irq uint8 }
//  ^^^^^ entity.name.type.variant
//                ^^^ variable.other.field
    Failed syscall.Errno
}

type Number interface {
//          ^^^^^^^^^ keyword.interface
    ~int | ~uint64
//  ^ keyword.operator.type-set
//       ^ keyword.operator.type-set
    String() string
//  ^^^^^^ entity.name.function
}

type UART struct {
    data   volatile[uint8]
//         ^^^^^^^^ support.type.builtin.constructor
    page   aligned[[4096]byte, 4096]
//         ^^^^^^^ support.type.builtin.constructor
    queue  ring[uint8, 64]
//         ^^^^ support.type.builtin.constructor
    share  shared[Node]
//         ^^^^^^ support.type.builtin.constructor
    sync   synced[Node]
//         ^^^^^^ support.type.builtin.constructor
    back   weak[shared[Node]]
//         ^^^^ support.type.builtin.constructor
//              ^^^^^^ support.type.builtin.constructor
}

func New[T any](a alloc.Allocator = alloc.Default()) *T {
//   ^^^ entity.name.function
//       ^ entity.name.type.parameter
//              ^ variable.parameter
//                      ^^^^^^^^^ entity.name.type
//                                ^ keyword.operator.assignment.default
//                                        ^^^^^^^ entity.name.function
//                                                    ^ entity.name.type
    return nil
}

func (v *Vec[T]) Map[U any](fn func(T) U, a alloc.Allocator = alloc.Default()) Vec[U] {
//    ^ variable.parameter.receiver
//       ^^^ entity.name.type
//               ^^^ entity.name.function
//                   ^ entity.name.type.parameter
//                          ^^ variable.parameter
//                             ^^^^ keyword.function
//                                  ^ entity.name.type
//                                     ^ entity.name.type
//                                        ^ variable.parameter
    out := v.Map[int](func(x T) int { return 0 })
//  ^^^ variable.other.declaration
//      ^^ keyword.operator.assignment
//           ^^^ entity.name.function
//               ^^^ support.type.builtin
//                    ^^^^ keyword.function
//                         ^ variable.parameter
    n := take(&v.data)
//       ^^^^ support.function.builtin
//               ^^^^ variable.other.property
    m := len(v.data) + cap(out)
    min := 3
//  ^^^ variable.other.declaration !support.function
    _ = max(min, 4)
//      ^^^ support.function.builtin
//          ^^^ !support.function
    clear(v)
//  ^^^^^ support.function.builtin
    print("x")
//  ^^^^^ support.function.builtin
    println("y")
//  ^^^^^^^ support.function.builtin
    panic("z")
//  ^^^^^ support.function.builtin
    b := make([]byte, 0, 64, a)
//       ^^^^ support.function.builtin
    n := copy(b, []byte{'a', '\n', '\x41'})
//       ^^^^ support.function.builtin
//                      ^^^ string.quoted.single.rune
//                            ^^ constant.character.escape
//                                  ^^^^ constant.character.escape
    s := "tab\there %d %-5.2f\q"
//           ^^ constant.character.escape
//                  ^^ constant.other.placeholder
//                     ^^^^^^ constant.other.placeholder
//                           ^^ invalid.illegal.unknown-escape
    r := `raw\n %d`
//        ^^^^^ string.quoted.raw !constant.character.escape
    var hex, oct, bin = 0xFF_FF, 0o755, 0b1010_0101
//  ^^^ keyword.var
//      ^^^ variable.other.declaration
//                      ^^^^^^^ constant.numeric.integer.hexadecimal
//                               ^^^^^ constant.numeric.integer.octal
//                                      ^^^^^^^^^^^ constant.numeric.integer.binary
    const big = 1_000_000
//  ^^^^^ keyword.const
//        ^^^ variable.other.constant
//              ^^^^^^^^^ constant.numeric.integer.decimal
    fl := 1.5e-3 + .25 + 6. + 0x1p-2
//        ^^^^^^ constant.numeric.float.decimal
//                 ^^^ constant.numeric.float.decimal
//                       ^^ constant.numeric.float.decimal
//                            ^^^^^^ constant.numeric.float.hexadecimal
    bits.Set(&flags, 3)
//  ^^^^ !keyword.bits
    shared := 1
//  ^^^^^^ variable.other.declaration !support.type
    c := complex(1, 2)
//       ^^^^^^^ !support.function
    var z complex128
//        ^^^^^^^^^^ !support.type
    var legacy map[string]int
//             ^^^ invalid.deprecated.removed
    ch := make(chan int, 1)
//             ^^^^ keyword.channel
    ch <- 1
//     ^^ keyword.operator.channel
    x := <-ch
//       ^^ keyword.operator.channel
    x &^= 3
//    ^^^ keyword.operator.assignment.compound
    x <<= 1
//    ^^^ keyword.operator.assignment.compound
    go worker(ch)
//  ^^ keyword.control
//     ^^^^^^ entity.name.function
    defer close(ch)
//        ^^^^^ support.function.builtin
    select {
//  ^^^^^^ keyword.control
    case v := <-ch:
//  ^^^^ keyword.control
//            ^^ keyword.operator.channel
        _ = v
    default:
    }
outer:
// <----- entity.name.label
    for i := range 10 {
        if i > 5 {
            break outer
//                ^^^^^ entity.name.label
        }
        continue outer
    }
    goto outer
//       ^^^^^ entity.name.label
    switch e := err.(type) {
//                   ^^^^ keyword.type
    case nil:
    }
    p := unsafe.Pointer(&x)
//              ^^^^^^^ support.type.builtin.unsafe
    sz := unsafe.Sizeof(x)
//               ^^^^^^ support.function.builtin.unsafe
    ok := true && !false || nil == nil
//        ^^^^ constant.language
//             ^^ keyword.operator.logical
//                       ^^ keyword.operator.logical
//                              ^^ keyword.operator.comparison
    t := Color.Red
    f.Printf("%v", args...)
//    ^^^^^^ entity.name.function
//            ^^ constant.other.placeholder
//                     ^^^ keyword.operator.ellipsis
    return Vec[U]{}
}

type (
    Handle owner uintptr
//  ^^^^^^ entity.name.type
//         ^^^^^ keyword.owner
    Pair[K comparable, V any] struct {
//  ^^^^ entity.name.type
//       ^ entity.name.type.parameter
//         ^^^^^^^^^^ support.type.builtin
//                     ^ entity.name.type.parameter
        key K
    }
    Callback func(n int) error
//  ^^^^^^^^ entity.name.type
//           ^^^^ keyword.function
//                ^ variable.parameter
)

type Resource owner interface {
//            ^^^^^ keyword.owner
    Read(p []byte) (n int, err error)
//  ^^^^ entity.name.function
//       ^ variable.parameter
//                         ^^^ variable.parameter
}

type Result[T any] union{ T | syscall.Errno }
//                 ^^^^^ keyword.union
//                          ^ keyword.operator.type-set

type PTE packed union uint64 {
//       ^^^^^^ keyword.packed
//              ^^^^^ keyword.union
    present bits[bool, 1]
//          ^^^^ keyword.bits
    Mapped case true {
//         ^^^^ keyword.control
//              ^^^^ constant.language
        frame bits[uint64, 40]
    }
}

func Index[S ~[]E, E comparable](s S, v E) (int, bool)
//         ^ entity.name.type.parameter
//           ^ keyword.operator.type-set
//              ^ entity.name.type !entity.name.type.parameter
//                 ^ entity.name.type.parameter
//                               ^ variable.parameter
//                                 ^ entity.name.type

func split(
    a, b int,
//  ^ variable.parameter
//     ^ variable.parameter
    rest ...string,
//  ^^^^ variable.parameter
//       ^^^ keyword.operator.ellipsis
) (q int, err error) {
// ^ variable.parameter
    switch c {
    case Red, Green:
    }
    return
}
