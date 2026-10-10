//pu:build linux

package fmt

import "pluto/os"

// Printf formats to standard output.
func Printf(format string, args ...any) (int, error) {
	return Fprintf(os.Stdout, format, args...)
}

// Print formats args with %v to standard output.
func Print(args ...any) (int, error) { return Fprint(os.Stdout, args...) }

// Println formats args with %v to standard output, separated by spaces and
// followed by a newline.
func Println(args ...any) (int, error) { return Fprintln(os.Stdout, args...) }
