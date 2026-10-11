// Package flag parses command-line flags, as Go's package flag does:
// -flag, -flag=x, -flag x (not for booleans) and --flag, stopping at the
// first non-flag argument or after "--".
//
// The differences from Go:
//
//   - A FlagSet owns the memory of its flags and of the variables that
//     Bool, Int, String and the like return: it is an owner (language spec
//     section 6.4), released with Drop. NewFlagSet returns a FlagSet, not a
//     *FlagSet. Declaring it owner is a one-line change once the keyword
//     lands (roadmap item 51).
//   - There are no Float64 flags yet (pluto/strconv has no ParseFloat).
package flag

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/errors"
	"pluto/fmt"
	"pluto/io"
	"pluto/os"
	"pluto/rt"
	"pluto/sort"
	"pluto/strconv"
	"pluto/time"
	"pluto/vec"
)

// ErrHelp is reported by Parse when -help or -h is given and not defined.
var ErrHelp = errors.New("flag: help requested")

// errParse and errRange are wrapped by the errors Set reports.
var (
	errParse = errors.New("parse error")
	errRange = errors.New("value out of range")
)

// A Value is the value of a flag: Set parses and stores a new value, and
// String formats the current one.
type Value interface {
	String() string
	Set(string) error
}

// boolFlag is a Value that needs no argument (-flag means -flag=true).
type boolFlag interface {
	Value
	IsBoolFlag() bool
}

type boolValue struct{ p *bool }

func (b *boolValue) Set(s string) error {
	v, err := parseBool(s)
	if err != nil {
		return err
	}
	*b.p = v
	return nil
}

func (b *boolValue) String() string {
	if *b.p {
		return "true"
	}
	return "false"
}

func (b *boolValue) IsBoolFlag() bool { return true }

func parseBool(s string) (bool, error) {
	switch s {
	case "1", "t", "T", "true", "TRUE", "True":
		return true, nil
	case "0", "f", "F", "false", "FALSE", "False":
		return false, nil
	}
	return false, errParse
}

type intValue struct {
	p *int64
	n *int // set for an int flag
}

func (v *intValue) Set(s string) error {
	x, err := strconv.ParseInt(s, 0, 64) // int is 64 bits on every target
	if err != nil {
		if err == strconv.ErrRange {
			return errRange
		}
		return errParse
	}
	if v.n != nil {
		*v.n = int(x)
	} else {
		*v.p = x
	}
	return nil
}

func (v *intValue) String() string {
	x := int64(0)
	if v.n != nil {
		x = int64(*v.n)
	} else {
		x = *v.p
	}
	return strconv.FormatInt(x, 10)
}

type uintValue struct {
	p *uint64
	n *uint // set for a uint flag
}

func (v *uintValue) Set(s string) error {
	x, err := strconv.ParseUint(s, 0, 64)
	if err != nil {
		if err == strconv.ErrRange {
			return errRange
		}
		return errParse
	}
	if v.n != nil {
		*v.n = uint(x)
	} else {
		*v.p = x
	}
	return nil
}

func (v *uintValue) String() string {
	x := uint64(0)
	if v.n != nil {
		x = uint64(*v.n)
	} else {
		x = *v.p
	}
	return strconv.FormatUint(x, 10)
}

type stringValue struct{ p *string }

func (v *stringValue) Set(s string) error {
	*v.p = s
	return nil
}

func (v *stringValue) String() string { return *v.p }

type durationValue struct {
	p *time.Duration
}

func (v *durationValue) Set(s string) error {
	d, err := time.ParseDuration(s)
	if err != nil {
		return errParse
	}
	*v.p = d
	return nil
}

func (v *durationValue) String() string { return v.p.String() }

// A Flag is a defined flag.
type Flag struct {
	Name     string // as it appears on the command line
	Usage    string // help message
	Value    Value  // value as set
	DefValue string // default value, as text

	kind string // type name for PrintDefaults, or "" for booleans
	zero string // the type's zero value, as text
}

// ErrorHandling says what Parse does when parsing fails.
type ErrorHandling int

// The choices.
const (
	ContinueOnError ErrorHandling = iota // return a descriptive error
	ExitOnError                          // exit with status 2 (0 for -help)
	PanicOnError                         // panic with the error
)

// A FlagSet is a set of defined flags. It owns their memory, so it is an
// owner (language spec section 7.18), released by Drop.
type FlagSet owner struct {
	// Usage prints a usage message when parsing fails; nil means the
	// default, a header and PrintDefaults.
	Usage func()

	name     string
	handling ErrorHandling
	parsed   bool
	mem      arena.Chunked
	flags    vec.Vec[*Flag]
	actual   vec.Vec[*Flag] // flags that were set
	args     []string       // arguments after the flags
	output   io.Writer
	osArgs   vec.Vec[string]  // the process's arguments, for CommandLine
	strs     vec.Vec[*string] // the string variables String allocated in mem
}

// NewFlagSet returns an empty FlagSet with the given name and error
// handling. Its memory comes from a, by default the default allocator.
func NewFlagSet(name string, handling ErrorHandling, a alloc.Allocator = alloc.Default()) FlagSet {
	return FlagSet{
		name:     name,
		handling: handling,
		mem:      arena.NewChunked(1024, a),
		flags:    vec.New[*Flag](a),
		actual:   vec.New[*Flag](a),
		strs:     vec.New[*string](a),
	}
}

// Drop frees the FlagSet's flags and the variables it allocated, releasing
// the strings in them (language spec section 18.8), whose memory the arena
// frees without looking at it.
func (f *FlagSet) Drop() {
	for _, fl := range f.flags.Slice() {
		rt.ReleaseValues(fl, 1)
	}
	for _, p := range f.strs.Slice() {
		rt.ReleaseValues(p, 1)
	}
	f.strs.Free()
	f.flags.Free()
	f.actual.Free()
	f.osArgs.Free()
	mem := take(&f.mem)
	mem.Drop()
}

// Name returns the FlagSet's name.
func (f *FlagSet) Name() string { return f.name }

// ErrorHandling returns the FlagSet's error handling.
func (f *FlagSet) ErrorHandling() ErrorHandling { return f.handling }

// Output returns where usage and error messages go: os.Stderr unless
// SetOutput changed it.
func (f *FlagSet) Output() io.Writer {
	if f.output == nil {
		return os.Stderr
	}
	return f.output
}

// SetOutput sets where usage and error messages go; nil means os.Stderr.
func (f *FlagSet) SetOutput(w io.Writer) { f.output = w }

// Parsed reports whether Parse has been called.
func (f *FlagSet) Parsed() bool { return f.parsed }

// Args returns the arguments after the flags, a view of Parse's argument.
func (f *FlagSet) Args() []string { return f.args }

// NArg returns the number of arguments after the flags.
func (f *FlagSet) NArg() int { return len(f.args) }

// Arg returns argument i after the flags, or "".
func (f *FlagSet) Arg(i int) string {
	if i < 0 || i >= len(f.args) {
		return ""
	}
	return f.args[i]
}

// NFlag returns the number of flags that have been set.
func (f *FlagSet) NFlag() int { return f.actual.Len() }

// Lookup returns the named flag, or nil.
func (f *FlagSet) Lookup(name string) *Flag {
	for _, fl := range f.flags.Slice() {
		if fl.Name == name {
			return fl
		}
	}
	return nil
}

// Set sets the named flag.
func (f *FlagSet) Set(name, value string) error {
	fl := f.Lookup(name)
	if fl == nil {
		return fmt.Errorf("no such flag -%s", name)
	}
	if err := fl.Value.Set(value); err != nil {
		return err
	}
	for _, a := range f.actual.Slice() {
		if a == fl {
			return nil
		}
	}
	f.actual.Push(fl)
	return nil
}

// VisitAll calls fn for every defined flag, in lexical order.
func (f *FlagSet) VisitAll(fn func(*Flag)) {
	v := vec.WithCapacity[*Flag](f.flags.Len())
	v.Append(f.flags.Slice()...)
	all := v.Slice()
	sort.SortFunc(all, func(a, b *Flag) bool { return a.Name < b.Name })
	for _, fl := range all {
		fn(fl)
	}
	v.Free()
}

// Visit calls fn for every flag that has been set, in lexical order.
func (f *FlagSet) Visit(fn func(*Flag)) {
	v := vec.WithCapacity[*Flag](f.actual.Len())
	v.Append(f.actual.Slice()...)
	all := v.Slice()
	sort.SortFunc(all, func(a, b *Flag) bool { return a.Name < b.Name })
	for _, fl := range all {
		fn(fl)
	}
	v.Free()
}

// Var defines a flag with the given name and usage whose value is value.
// It panics if the name is already defined or is not valid.
func (f *FlagSet) Var(value Value, name string, usage string) {
	f.define(value, name, usage, "value", "")
}

func (f *FlagSet) define(value Value, name, usage, kind, zero string) {
	if name == "" || name[0] == '-' {
		panic("flag: flag name must be non-empty and not start with '-'")
	}
	for i := 0; i < len(name); i++ {
		if name[i] == '=' {
			panic("flag: flag name contains '='")
		}
	}
	if f.Lookup(name) != nil {
		panic("flag: flag redefined")
	}
	fl := alloc.New[Flag](&f.mem)
	fl.Name = name
	fl.Usage = usage
	fl.Value = value
	fl.DefValue = value.String()
	fl.kind = kind
	fl.zero = zero
	f.flags.Push(fl)
}

// BoolVar defines a bool flag stored in *p, with default value.
func (f *FlagSet) BoolVar(p *bool, name string, value bool, usage string) {
	*p = value
	v := alloc.New[boolValue](&f.mem)
	v.p = p
	f.define(v, name, usage, "", "false")
}

// Bool defines a bool flag and returns its variable, which f owns.
func (f *FlagSet) Bool(name string, value bool, usage string) *bool {
	p := alloc.New[bool](&f.mem)
	f.BoolVar(p, name, value, usage)
	return p
}

// IntVar defines an int flag stored in *p, with default value.
func (f *FlagSet) IntVar(p *int, name string, value int, usage string) {
	*p = value
	v := alloc.New[intValue](&f.mem)
	v.n = p
	f.define(v, name, usage, "int", "0")
}

// Int defines an int flag and returns its variable, which f owns.
func (f *FlagSet) Int(name string, value int, usage string) *int {
	p := alloc.New[int](&f.mem)
	f.IntVar(p, name, value, usage)
	return p
}

// Int64Var defines an int64 flag stored in *p, with default value.
func (f *FlagSet) Int64Var(p *int64, name string, value int64, usage string) {
	*p = value
	v := alloc.New[intValue](&f.mem)
	v.p = p
	f.define(v, name, usage, "int", "0")
}

// Int64 defines an int64 flag and returns its variable, which f owns.
func (f *FlagSet) Int64(name string, value int64, usage string) *int64 {
	p := alloc.New[int64](&f.mem)
	f.Int64Var(p, name, value, usage)
	return p
}

// UintVar defines a uint flag stored in *p, with default value.
func (f *FlagSet) UintVar(p *uint, name string, value uint, usage string) {
	*p = value
	v := alloc.New[uintValue](&f.mem)
	v.n = p
	f.define(v, name, usage, "uint", "0")
}

// Uint defines a uint flag and returns its variable, which f owns.
func (f *FlagSet) Uint(name string, value uint, usage string) *uint {
	p := alloc.New[uint](&f.mem)
	f.UintVar(p, name, value, usage)
	return p
}

// Uint64Var defines a uint64 flag stored in *p, with default value.
func (f *FlagSet) Uint64Var(p *uint64, name string, value uint64, usage string) {
	*p = value
	v := alloc.New[uintValue](&f.mem)
	v.p = p
	f.define(v, name, usage, "uint", "0")
}

// Uint64 defines a uint64 flag and returns its variable, which f owns.
func (f *FlagSet) Uint64(name string, value uint64, usage string) *uint64 {
	p := alloc.New[uint64](&f.mem)
	f.Uint64Var(p, name, value, usage)
	return p
}

// StringVar defines a string flag stored in *p, with default value. The
// value a command line gives it is a view of Parse's argument.
func (f *FlagSet) StringVar(p *string, name string, value string, usage string) {
	*p = value
	v := alloc.New[stringValue](&f.mem)
	v.p = p
	f.define(v, name, usage, "string", "")
}

// String defines a string flag and returns its variable, which f owns.
func (f *FlagSet) String(name string, value string, usage string) *string {
	p := alloc.New[string](&f.mem)
	f.strs.Push(p)
	f.StringVar(p, name, value, usage)
	return p
}

// DurationVar defines a time.Duration flag stored in *p, with default
// value; it accepts what time.ParseDuration does.
func (f *FlagSet) DurationVar(p *time.Duration, name string, value time.Duration, usage string) {
	*p = value
	v := alloc.New[durationValue](&f.mem)
	v.p = p
	f.define(v, name, usage, "duration", "0s")
}

// Duration defines a time.Duration flag and returns its variable, which f
// owns.
func (f *FlagSet) Duration(name string, value time.Duration, usage string) *time.Duration {
	p := alloc.New[time.Duration](&f.mem)
	f.DurationVar(p, name, value, usage)
	return p
}

// UnquoteUsage returns the name for the flag's argument in usage messages
// and the usage without its quotes: the first `backquoted` word of the
// usage, or else the flag's type ("" for a boolean). The name is a view of
// the usage.
func UnquoteUsage(fl *Flag) (name string, usage string) {
	usage = fl.Usage
	for i := 0; i < len(usage); i++ {
		if usage[i] == '`' {
			for j := i + 1; j < len(usage); j++ {
				if usage[j] == '`' {
					return usage[i+1 : j], usage // the caller drops the quotes
				}
			}
			break
		}
	}
	return fl.kind, usage
}

// PrintDefaults prints the defined flags, sorted, with their usage and
// non-zero defaults, in Go's format.
func (f *FlagSet) PrintDefaults() {
	w := f.Output()
	f.VisitAll(func(fl *Flag) {
		// The flag and its argument's name, written as they are, so that
		// no length is too long.
		io.WriteString(w, "  -")
		io.WriteString(w, fl.Name)
		n := 3 + len(fl.Name)
		name, usage := UnquoteUsage(fl)
		if name != "" {
			io.WriteString(w, " ")
			io.WriteString(w, name)
			n += 1 + len(name)
		}
		if n <= 4 {
			io.WriteString(w, "\t")
		} else {
			io.WriteString(w, "\n    \t")
		}
		// The usage, with its backquotes removed and each newline
		// indented.
		quotes := 0
		for i := 0; i < len(usage); i++ {
			c := usage[i]
			switch {
			case c == '`' && quotes < 2:
				quotes++
			case c == '\n':
				io.WriteString(w, "\n    \t")
			default:
				io.WriteString(w, usage[i:i+1])
			}
		}
		if fl.DefValue != fl.zero {
			if fl.kind == "string" {
				fmt.Fprintf(w, " (default %q)", fl.DefValue)
			} else {
				fmt.Fprintf(w, " (default %s)", fl.DefValue)
			}
		}
		io.WriteString(w, "\n")
	})
}

// defaultUsage prints the default usage message.
func (f *FlagSet) defaultUsage() {
	if f.name == "" {
		io.WriteString(f.Output(), "Usage:\n")
	} else {
		fmt.Fprintf(f.Output(), "Usage of %s:\n", f.name)
	}
	f.PrintDefaults()
}

func (f *FlagSet) usage() {
	if f.Usage == nil {
		f.defaultUsage()
	} else {
		f.Usage()
	}
}

// failf reports a parse error: it prints the message and the usage and
// returns the error.
func (f *FlagSet) failf(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	fmt.Fprintln(f.Output(), err.Error())
	f.usage()
	return err
}

// parseOne parses one flag and reports whether there was one.
func (f *FlagSet) parseOne() (bool, error) {
	if len(f.args) == 0 {
		return false, nil
	}
	s := f.args[0]
	if len(s) < 2 || s[0] != '-' {
		return false, nil
	}
	numMinuses := 1
	if s[1] == '-' {
		numMinuses++
		if len(s) == 2 { // "--" ends the flags
			f.args = f.args[1:]
			return false, nil
		}
	}
	name := s[numMinuses:]
	if len(name) == 0 || name[0] == '-' || name[0] == '=' {
		return false, f.failf("bad flag syntax: %s", s)
	}
	f.args = f.args[1:]
	hasValue := false
	value := ""
	for i := 1; i < len(name); i++ {
		if name[i] == '=' {
			value = name[i+1:]
			hasValue = true
			name = name[0:i]
			break
		}
	}
	fl := f.Lookup(name)
	if fl == nil {
		if name == "help" || name == "h" {
			f.usage()
			return false, ErrHelp
		}
		return false, f.failf("flag provided but not defined: -%s", name)
	}
	if bv, ok := fl.Value.(boolFlag); ok && bv.IsBoolFlag() {
		if hasValue {
			if err := fl.Value.Set(value); err != nil {
				return false, f.failf("invalid boolean value %q for -%s: %v", value, name, err)
			}
		} else if err := fl.Value.Set("true"); err != nil {
			return false, f.failf("invalid boolean flag %s: %v", name, err)
		}
	} else {
		if !hasValue && len(f.args) > 0 {
			hasValue = true
			value, f.args = f.args[0], f.args[1:]
		}
		if !hasValue {
			return false, f.failf("flag needs an argument: -%s", name)
		}
		if err := fl.Value.Set(value); err != nil {
			return false, f.failf("invalid value %q for flag -%s: %v", value, name, err)
		}
	}
	for _, a := range f.actual.Slice() {
		if a == fl {
			return true, nil
		}
	}
	f.actual.Push(fl)
	return true, nil
}

// Parse parses flags from arguments, which must not include the command
// name. It must be called after every flag is defined and before the flags
// are read. The return value is ErrHelp for an undefined -help or -h.
func (f *FlagSet) Parse(arguments []string) error {
	f.parsed = true
	f.args = arguments
	for {
		seen, err := f.parseOne()
		if seen {
			continue
		}
		if err == nil {
			break
		}
		switch f.handling {
		case ContinueOnError:
			return err
		case ExitOnError:
			if err == ErrHelp {
				os.Exit(0)
			}
			os.Exit(2)
		case PanicOnError:
			panic(err)
		}
	}
	return nil
}

// CommandLine is the set of flags of the command line, named after the
// program. The package-level functions use it.
var CommandLine = NewFlagSet(os.Arg(0), ExitOnError)

// Bool defines a bool flag on the command line.
func Bool(name string, value bool, usage string) *bool { return CommandLine.Bool(name, value, usage) }

// BoolVar defines a bool flag on the command line, stored in *p.
func BoolVar(p *bool, name string, value bool, usage string) {
	CommandLine.BoolVar(p, name, value, usage)
}

// Int defines an int flag on the command line.
func Int(name string, value int, usage string) *int { return CommandLine.Int(name, value, usage) }

// IntVar defines an int flag on the command line, stored in *p.
func IntVar(p *int, name string, value int, usage string) { CommandLine.IntVar(p, name, value, usage) }

// Int64 defines an int64 flag on the command line.
func Int64(name string, value int64, usage string) *int64 {
	return CommandLine.Int64(name, value, usage)
}

// Uint defines a uint flag on the command line.
func Uint(name string, value uint, usage string) *uint { return CommandLine.Uint(name, value, usage) }

// String defines a string flag on the command line.
func String(name string, value string, usage string) *string {
	return CommandLine.String(name, value, usage)
}

// StringVar defines a string flag on the command line, stored in *p.
func StringVar(p *string, name string, value string, usage string) {
	CommandLine.StringVar(p, name, value, usage)
}

// Duration defines a time.Duration flag on the command line.
func Duration(name string, value time.Duration, usage string) *time.Duration {
	return CommandLine.Duration(name, value, usage)
}

// Var defines a flag on the command line with the given Value.
func Var(value Value, name string, usage string) { CommandLine.Var(value, name, usage) }

// Parse parses the command line's flags from the process's arguments.
func Parse() {
	CommandLine.osArgs.Free()
	CommandLine.osArgs = os.Args()
	CommandLine.Parse(CommandLine.osArgs.Slice()[1:])
}

// Parsed reports whether the command line has been parsed.
func Parsed() bool { return CommandLine.Parsed() }

// Args returns the command line's arguments after the flags.
func Args() []string { return CommandLine.Args() }

// NArg returns the number of arguments after the flags.
func NArg() int { return CommandLine.NArg() }

// Arg returns argument i after the flags, or "". An argument is any bytes
// but NUL, and a string holds only valid UTF-8 (language spec section 18.8),
// so a command-line argument that is not valid UTF-8 (a file name, say)
// comes back with U+FFFD for each invalid byte; ArgBytes returns it exactly.
func Arg(i int) string { return CommandLine.Arg(i) }

// ArgBytes returns the exact bytes of argument i after the flags, or nil. It
// is a view of the process's arguments (os.ArgBytes), valid for the life of
// the program; do not modify it. It is meaningful only when the command
// line was parsed by Parse, which takes the arguments from the process, and
// returns nil before that.
func ArgBytes(i int) []byte {
	if i < 0 || i >= CommandLine.NArg() || CommandLine.osArgs.Len() == 0 {
		return nil
	}
	return os.ArgBytes(os.NArgs() - CommandLine.NArg() + i)
}

// NFlag returns the number of command-line flags that have been set.
func NFlag() int { return CommandLine.NFlag() }

// Lookup returns the named command-line flag, or nil.
func Lookup(name string) *Flag { return CommandLine.Lookup(name) }

// Set sets the named command-line flag.
func Set(name, value string) error { return CommandLine.Set(name, value) }

// PrintDefaults prints the command-line flags' defaults.
func PrintDefaults() { CommandLine.PrintDefaults() }
