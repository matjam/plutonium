package bytes

// Equal returns true if two byte slices are equal.
func Equal(a, b []byte) bool {
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

// Contains returns true if needle is a subsequence of haystack.
func Contains(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// IndexByte returns the first index of b in s, or -1.
func IndexByte(s []byte, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// TrimRight returns s without trailing bytes in cutset.
func TrimRight(s []byte, cutset []byte) []byte {
	end := len(s)
	for end > 0 && IndexByte(cutset, s[end-1]) >= 0 {
		end--
	}
	return s[:end]
}

// Copy copies bytes from src to dst and returns the number of bytes copied.
func Copy(dst, src []byte) int {
	n := len(dst)
	if len(src) < n {
		n = len(src)
	}
	for i := 0; i < n; i++ {
		dst[i] = src[i]
	}
	return n
}
