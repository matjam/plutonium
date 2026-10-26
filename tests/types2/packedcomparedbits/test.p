package main

type Full packed uint8 { a bits[uint, 4]; b bits[uint, 4] }
type Reg packed uint32 { mode bits[uint, 4]; _ bits[uint, 4]; on bits[bool, 1] }
type Hdr packed [3]byte { a bits[uint8, 3]; _ bits[uint8, 5]; b uint8 }
type Inner packed uint8 { v bits[uint, 2]; _ bits[uint, 6] }
type Outer packed [3]byte { i Inner; j [2]Inner }
type Flags packed uint8 { f bits { A; _; C } }
type U packed union uint8 { tag bits[uint, 2]; X case 0 { v bits[uint, 4] }; Y case 1 {} }
