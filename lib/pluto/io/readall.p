package io

import (
	"pluto/alloc"
	"pluto/vec"
)

// ReadAll reads from r until EOF and returns what it read in a vec.Vec that
// allocates from a (by default the default allocator); the caller owns it.
// Reaching EOF is success: the error is nil then, and otherwise the error
// that stopped the reading, with the data read before it.
func ReadAll(r Reader, a alloc.Allocator = alloc.Default()) (vec.Vec[byte], error) {
	data := vec.New[byte](a)
	var chunk [8 << 10]byte
	for {
		n, err := r.Read(chunk[:])
		data.Append(chunk[:n]...)
		if err == EOF {
			return data, nil
		}
		if err != nil {
			return data, err
		}
		if n == 0 {
			// A Reader that returns neither data nor an error would loop
			// forever; treat it as the end of input.
			return data, nil
		}
	}
}
