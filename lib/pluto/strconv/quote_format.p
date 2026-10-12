package strconv

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/unicode/utf8"
	"pluto/vec"
	"unsafe"
)

const (
	quotePlain = iota
	quoteASCII
	quoteGraphic
)

// Quote returns a double-quoted string literal representing s. Control
// characters and non-printable runes use escapes; IsPrint defines printable.
func Quote(s string) string { return quoteString(s, quotePlain) }

// QuoteToASCII returns a double-quoted string literal representing s,
// escaping every non-ASCII rune and non-printable character.
func QuoteToASCII(s string) string { return quoteString(s, quoteASCII) }

// QuoteToGraphic returns a double-quoted string literal representing s,
// leaving Unicode graphic characters unchanged and escaping the rest.
func QuoteToGraphic(s string) string { return quoteString(s, quoteGraphic) }

// AppendQuote appends the literal made by Quote to dst.
func AppendQuote[D vec.Appender[byte]](dst D, s string) {
	appendQuoteString(dst, s, quotePlain)
}

// AppendQuoteToASCII appends the literal made by QuoteToASCII to dst.
func AppendQuoteToASCII[D vec.Appender[byte]](dst D, s string) {
	appendQuoteString(dst, s, quoteASCII)
}

// AppendQuoteToGraphic appends the literal made by QuoteToGraphic to dst.
func AppendQuoteToGraphic[D vec.Appender[byte]](dst D, s string) {
	appendQuoteString(dst, s, quoteGraphic)
}

// QuoteRune returns a single-quoted rune literal representing r, escaping
// non-printable runes. Invalid code points become U+FFFD.
func QuoteRune(r rune) string { return quoteRune(r, quotePlain) }

// QuoteRuneToASCII returns a single-quoted rune literal representing r,
// escaping non-ASCII and non-printable runes. Invalid code points become U+FFFD.
func QuoteRuneToASCII(r rune) string { return quoteRune(r, quoteASCII) }

// QuoteRuneToGraphic returns a single-quoted rune literal representing r,
// leaving graphic characters unchanged. Invalid code points become U+FFFD.
func QuoteRuneToGraphic(r rune) string { return quoteRune(r, quoteGraphic) }

// AppendQuoteRune appends the literal made by QuoteRune to dst.
func AppendQuoteRune[D vec.Appender[byte]](dst D, r rune) {
	appendQuoteRune(dst, r, quotePlain)
}

// AppendQuoteRuneToASCII appends the literal made by QuoteRuneToASCII to dst.
func AppendQuoteRuneToASCII[D vec.Appender[byte]](dst D, r rune) {
	appendQuoteRune(dst, r, quoteASCII)
}

// AppendQuoteRuneToGraphic appends the literal made by QuoteRuneToGraphic to dst.
func AppendQuoteRuneToGraphic[D vec.Appender[byte]](dst D, r rune) {
	appendQuoteRune(dst, r, quoteGraphic)
}

// escapedRune writes at most ten bytes. Measurement and emission share this
// decision so an exact-sized counted buffer needs no intermediate allocation.
func escapedRune(dst []byte, r rune, quote byte, mode int) int {
	const hex = "0123456789abcdef"
	if r == rune(quote) || r == '\\' {
		dst[0], dst[1] = '\\', byte(r)
		return 2
	}
	if mode == quoteASCII {
		if r < utf8.RuneSelf && IsPrint(r) {
			dst[0] = byte(r)
			return 1
		}
	} else if IsPrint(r) || mode == quoteGraphic && IsGraphic(r) {
		return utf8.EncodeRune(dst, r)
	}
	var short byte
	switch r {
	case '\a':
		short = 'a'
	case '\b':
		short = 'b'
	case '\f':
		short = 'f'
	case '\n':
		short = 'n'
	case '\r':
		short = 'r'
	case '\t':
		short = 't'
	case '\v':
		short = 'v'
	}
	if short != 0 {
		dst[0], dst[1] = '\\', short
		return 2
	}
	if r < ' ' || r == 0x7f {
		dst[0], dst[1], dst[2], dst[3] = '\\', 'x', hex[byte(r)>>4], hex[byte(r)&15]
		return 4
	}
	n := 6
	dst[0], dst[1] = '\\', 'u'
	if r >= 0x10000 {
		n = 10
		dst[1] = 'U'
	}
	for i := 2; i < n; i++ {
		dst[i] = hex[(r>>uint((n-i-1)*4))&15]
	}
	return n
}

// quotedUnit decodes the entire remaining byte view, so buffer boundaries
// never cut a rune. Malformed unsafe views escape one offending byte at a time.
func quotedUnit(dst []byte, src []byte, mode int) (int, int) {
	r, width := utf8.DecodeRune(src)
	if r == utf8.RuneError && width == 1 {
		const hex = "0123456789abcdef"
		dst[0], dst[1], dst[2], dst[3] = '\\', 'x', hex[src[0]>>4], hex[src[0]&15]
		return 4, 1
	}
	return escapedRune(dst, r, '"', mode), width
}

func quoteString(s string, mode int) string {
	src := unsafe.Slice(unsafe.StringData(s), len(s))
	var unit [10]byte
	n, ascii := 2, true
	for i := 0; i < len(src); {
		k, width := quotedUnit(unit[:], src[i:], mode)
		n += k
		ascii = ascii && unit[0] < utf8.RuneSelf
		i += width
	}
	a := alloc.Default()
	if a == nil {
		panic("strconv: no allocator: call alloc.SetDefault before building strings")
	}
	p, ctl := rt.NewStringBuffer(n, a)
	b := unsafe.Slice(p, n)
	b[0], b[n-1] = '"', '"'
	w := 1
	for i := 0; i < len(src); {
		k, width := quotedUnit(b[w:], src[i:], mode)
		w += k
		i += width
	}
	q := rt.MakeString(p, n, ctl, ascii)
	rt.ReleaseString(ctl)
	return q
}

func appendQuoteString[D vec.Appender[byte]](dst D, s string, mode int) {
	src := unsafe.Slice(unsafe.StringData(s), len(s))
	var buf [256]byte
	buf[0] = '"'
	w := 1
	for i := 0; i < len(src); {
		// Leave room for the largest escaped rune and the closing quote.
		if len(buf)-w < 11 {
			dst.Append(buf[:w]...)
			w = 0
		}
		k, width := quotedUnit(buf[w:], src[i:], mode)
		w += k
		i += width
	}
	buf[w] = '"'
	dst.Append(buf[:w+1]...)
}

func quotedRune(dst []byte, r rune, mode int) int {
	if !utf8.ValidRune(r) {
		r = utf8.RuneError
	}
	dst[0] = '\''
	n := 1 + escapedRune(dst[1:], r, '\'', mode)
	dst[n] = '\''
	return n + 1
}

func quoteRune(r rune, mode int) string {
	var buf [12]byte
	n := quotedRune(buf[:], r, mode)
	return rt.StringFromSlice(buf[:n], alloc.Default())
}

func appendQuoteRune[D vec.Appender[byte]](dst D, r rune, mode int) {
	var buf [12]byte
	n := quotedRune(buf[:], r, mode)
	dst.Append(buf[:n]...)
}
