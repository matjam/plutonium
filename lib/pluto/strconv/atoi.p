package strconv

import "pluto/errors"

// ErrSyntax indicates that a value does not have the right syntax.
var ErrSyntax = &errors.Text{Msg: "invalid syntax"}

// ErrRange indicates that a value is out of range.
var ErrRange = &errors.Text{Msg: "value out of range"}

// ParseUint parses s in the given base (0 means: infer from a 0x, 0o, 0b or
// 0 prefix) as an unsigned integer of bitSize bits (0 means 64).
func ParseUint(s string, base int, bitSize int) (uint64, error) {
	if len(s) == 0 {
		return 0, ErrSyntax
	}
	if bitSize == 0 {
		bitSize = 64
	}
	underscores := false
	if base == 0 {
		base = 10
		if s[0] == '0' {
			switch {
			case len(s) >= 3 && (s[1] == 'x' || s[1] == 'X'):
				base, s = 16, s[2:]
			case len(s) >= 3 && (s[1] == 'o' || s[1] == 'O'):
				base, s = 8, s[2:]
			case len(s) >= 3 && (s[1] == 'b' || s[1] == 'B'):
				base, s = 2, s[2:]
			default:
				base, s = 8, s[1:]
			}
		}
		underscores = true
	}
	maxVal := uint64(1)<<uint(bitSize) - 1
	if bitSize == 64 {
		maxVal = ^uint64(0)
	}
	cutoff := ^uint64(0)/uint64(base) + 1
	var n uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		var d byte
		switch {
		case c == '_' && underscores:
			continue
		case '0' <= c && c <= '9':
			d = c - '0'
		case 'a' <= c && c <= 'z':
			d = c - 'a' + 10
		case 'A' <= c && c <= 'Z':
			d = c - 'A' + 10
		default:
			return 0, ErrSyntax
		}
		if int(d) >= base {
			return 0, ErrSyntax
		}
		if n >= cutoff {
			return maxVal, ErrRange
		}
		n *= uint64(base)
		n1 := n + uint64(d)
		if n1 < n || n1 > maxVal {
			return maxVal, ErrRange
		}
		n = n1
	}
	return n, nil
}

// ParseInt parses s as a signed integer (see ParseUint).
func ParseInt(s string, base int, bitSize int) (int64, error) {
	if len(s) == 0 {
		return 0, ErrSyntax
	}
	if bitSize == 0 {
		bitSize = 64
	}
	neg := false
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		neg = true
		s = s[1:]
	}
	u, err := ParseUint(s, base, bitSize)
	if err != nil && err != error(ErrRange) {
		return 0, err
	}
	cutoff := uint64(1) << uint(bitSize-1)
	if !neg && u >= cutoff {
		return int64(cutoff - 1), ErrRange
	}
	if neg && u > cutoff {
		return -int64(cutoff), ErrRange
	}
	if neg {
		return -int64(u), nil
	}
	return int64(u), nil
}

// Atoi is ParseInt(s, 10, 0) converted to int.
func Atoi(s string) (int, error) {
	n, err := ParseInt(s, 10, 0)
	return int(n), err
}
