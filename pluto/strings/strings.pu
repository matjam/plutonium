package strings

// Len returns the byte length of a string.
func Len(s string) int {
	return len(s)
}

// Equal returns true if two strings are equal.
func Equal(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// HasPrefix returns true if s starts with prefix.
func HasPrefix(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if s[i] != prefix[i] {
			return false
		}
	}
	return true
}

// HasSuffix returns true if s ends with suffix.
func HasSuffix(s, suffix string) bool {
	if len(suffix) > len(s) {
		return false
	}
	offset := len(s) - len(suffix)
	for i := 0; i < len(suffix); i++ {
		if s[offset+i] != suffix[i] {
			return false
		}
	}
	return true
}

// Index returns the index of the first occurrence of sub in s, or -1.
func Index(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	if len(sub) > len(s) {
		return -1
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// Contains returns true if sub is a substring of s.
func Contains(s, sub string) bool {
	return Index(s, sub) >= 0
}

// TrimSpace returns s with leading and trailing whitespace removed.
func TrimSpace(s string) string {
	start := 0
	for start < len(s) && isSpace(s[start]) {
		start++
	}
	end := len(s)
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
