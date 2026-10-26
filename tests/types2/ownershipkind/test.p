package main
import "unsafe"
type Own owner struct{ Data []byte }
type OwnIface owner interface{ Close() }
type Alias = OwnIface
type Error interface{ Error() string }
type Mixed struct { Text string; Data []byte }
type Counted struct { Text string; Ref shared[int] }
type Text union { Empty; Some string }
type MixedUnion union { Empty; Some string; Bytes []byte }
type Enum enum { First; Second }
type Bits packed uint8 { Low bits[uint8, 4]; High bits[uint8, 4] }
type Plain int
type Generic[T any] struct { V T }
type GenericOwner[T owner any] owner struct { V T }
var String string
var Map map[int]int
var Chan chan int
var Fn func()
var Slice []int
var Pointer *int
var Unsafe unsafe.Pointer
var Shared shared[int]
var Synced synced[int]
var Weak weak[shared[int]]
var Owned owned[int]
var Array [4]string
var Volatile volatile[uint32]
var Aligned aligned[string, 8]
var Ring ring[string, 8]
var Ordinary Generic[int]
var Owner GenericOwner[Own]
type Recursive struct { Next *Recursive; Text string }
