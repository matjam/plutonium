package main

// An alias marked owner (E3205).

type Buf owner struct{ b []byte }

type Other = owner Buf

func main() {}
