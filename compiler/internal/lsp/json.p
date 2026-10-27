// A minimal JSON reader and writer for the server's messages, in place of
// Go's encoding/json: values parse into a tree, which the protocol's
// decoders read field by field, and the protocol's encoders write their
// types directly, with encoding/json's spelling (field order, omitempty,
// null for a nil slice or pointer, HTML-safe string escapes).

package lsp

import (
	"pluto/strconv"
	"pluto/strings"
	"pluto/unicode/utf8"
	"compiler/compat"
	"compiler/pu"
)

type jsonKind int

const (
	jsonNull jsonKind = iota
	jsonBool
	jsonNumber
	jsonString
	jsonArray
	jsonObject
)

// A jsonValue is a parsed JSON value. raw is its text in the message.
type jsonValue struct {
	kind  jsonKind
	b     bool
	s     string // a string's value, or a number's text
	elems []*jsonValue
	keys  []string
	vals  []*jsonValue
	raw   []byte
}

// get returns the member of an object named key, or nil. Like
// encoding/json it matches names without regard to case, and a later
// member wins over an earlier one.
func (v *jsonValue) get(key string) *jsonValue {
	if v == nil || v.kind != jsonObject {
		return nil
	}
	var found *jsonValue
	for i, k := range v.keys {
		if k == key || strings.EqualFold(k, key) {
			found = v.vals[i]
		}
	}
	return found
}

// isNull reports whether v is absent or null.
func (v *jsonValue) isNull() bool { return v == nil || v.kind == jsonNull }

// kindName names a value's kind as encoding/json's errors do.
func (v *jsonValue) kindName() string {
	switch v.kind {
	case jsonBool:
		return "bool"
	case jsonNumber:
		return "number"
	case jsonString:
		return "string"
	case jsonArray:
		return "array"
	case jsonObject:
		return "object"
	}
	return "null"
}

// A jsonParser parses one JSON text.
type jsonParser struct {
	data []byte
	pos  int
}

// parseJSON parses a whole JSON text.
func parseJSON(data []byte) (*jsonValue, error) {
	if len(data) == 0 {
		return nil, compat.NewError("unexpected end of JSON input")
	}
	p := pu.New(jsonParser{data: data})
	v := p.value("beginning of value") or return
	p.space()
	if p.pos < len(p.data) {
		return nil, p.invalid("after top-level value")
	}
	return v, nil
}

func (p *jsonParser) space() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

// invalid returns encoding/json's error for an unexpected byte (or the
// end of the input) where context was expected.
func (p *jsonParser) invalid(context string) error {
	if p.pos >= len(p.data) {
		return compat.NewError("unexpected end of JSON input")
	}
	return compat.NewError(compat.Sprintfb("invalid character %s %s", compat.ArgS(quoteChar(p.data[p.pos])), compat.ArgS(context)))
}

// quoteChar formats a byte as encoding/json's errors do.
func quoteChar(c byte) string {
	if c == '\'' {
		return `'\''`
	}
	if c == '"' {
		return `'"'`
	}
	s := strconv.Quote(compat.RuneString(rune(c)))
	return compat.Concat("'", strings.Slice(s, 1, len(s)-1), "'")
}

func (p *jsonParser) value(context string) (*jsonValue, error) {
	p.space()
	if p.pos >= len(p.data) {
		return nil, p.invalid(context)
	}
	start := p.pos
	v := pu.New(jsonValue{})
	switch c := p.data[p.pos]; {
	case c == '{':
		v.kind = jsonObject
		p.pos++
		p.space()
		if p.pos < len(p.data) && p.data[p.pos] == '}' {
			p.pos++
			break
		}
		for {
			p.space()
			if p.pos >= len(p.data) || p.data[p.pos] != '"' {
				return nil, p.invalid("looking for beginning of object key string")
			}
			key := p.str() or return
			p.space()
			if p.pos >= len(p.data) || p.data[p.pos] != ':' {
				return nil, p.invalid("after object key")
			}
			p.pos++
			val := p.value("looking for beginning of value") or return
			v.keys = compat.Append(v.keys, key)
			v.vals = compat.Append(v.vals, val)
			p.space()
			if p.pos < len(p.data) && p.data[p.pos] == ',' {
				p.pos++
				continue
			}
			if p.pos < len(p.data) && p.data[p.pos] == '}' {
				p.pos++
				break
			}
			return nil, p.invalid("after object key:value pair")
		}
	case c == '[':
		v.kind = jsonArray
		p.pos++
		p.space()
		if p.pos < len(p.data) && p.data[p.pos] == ']' {
			p.pos++
			v.elems = compat.Clone([]*jsonValue{})
			break
		}
		for {
			e := p.value("looking for beginning of value") or return
			v.elems = compat.Append(v.elems, e)
			p.space()
			if p.pos < len(p.data) && p.data[p.pos] == ',' {
				p.pos++
				continue
			}
			if p.pos < len(p.data) && p.data[p.pos] == ']' {
				p.pos++
				break
			}
			return nil, p.invalid("after array element")
		}
	case c == '"':
		v.kind = jsonString
		v.s = p.str() or return
	case c == '-' || '0' <= c && c <= '9':
		v.kind = jsonNumber
		if err := p.number(); err != nil {
			return nil, err
		}
		v.s = compat.String(p.data[start:p.pos])
	case c == 't':
		v.kind, v.b = jsonBool, true
		if err := p.literal("true"); err != nil {
			return nil, err
		}
	case c == 'f':
		v.kind = jsonBool
		if err := p.literal("false"); err != nil {
			return nil, err
		}
	case c == 'n':
		if err := p.literal("null"); err != nil {
			return nil, err
		}
	default:
		if context == "beginning of value" {
			return nil, p.invalid("looking for beginning of value")
		}
		return nil, p.invalid(context)
	}
	v.raw = p.data[start:p.pos]
	return v, nil
}

func (p *jsonParser) literal(word string) error {
	for i := 0; i < len(word); i++ {
		if p.pos >= len(p.data) {
			return compat.NewError("unexpected end of JSON input")
		}
		if p.data[p.pos] != word[i] {
			return p.invalid(compat.Concat("in literal ", word, " (expecting ", quoteChar(word[i]), ")"))
		}
		p.pos++
	}
	return nil
}

func (p *jsonParser) digits() int {
	n := 0
	for p.pos < len(p.data) && '0' <= p.data[p.pos] && p.data[p.pos] <= '9' {
		p.pos++
		n++
	}
	return n
}

// number scans a number: -?(0|[1-9][0-9]*)(.[0-9]+)?([eE][+-]?[0-9]+)?
func (p *jsonParser) number() error {
	if p.data[p.pos] == '-' {
		p.pos++
		if p.pos >= len(p.data) || p.data[p.pos] < '0' || p.data[p.pos] > '9' {
			return p.invalid("in numeric literal")
		}
	}
	if p.data[p.pos] == '0' {
		p.pos++
	} else {
		p.digits()
	}
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++
		if p.digits() == 0 {
			return p.invalid("after decimal point in numeric literal")
		}
	}
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}
		if p.digits() == 0 {
			return p.invalid("in exponent of numeric literal")
		}
	}
	return nil
}

// str scans a string at p.pos, which is a quote, and returns its value.
// Invalid UTF-8 and unpaired surrogates become U+FFFD, as with
// encoding/json.
func (p *jsonParser) str() (string, error) {
	p.pos++ // the quote
	var b []byte
	for {
		if p.pos >= len(p.data) {
			return "", compat.NewError("unexpected end of JSON input")
		}
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			return compat.String(b), nil
		case c < 0x20:
			return "", p.invalid("in string literal")
		case c == '\\':
			p.pos++
			if p.pos >= len(p.data) {
				return "", compat.NewError("unexpected end of JSON input")
			}
			e := p.data[p.pos]
			p.pos++
			switch e {
			case '"', '\\', '/':
				b = compat.AppendPlain(b, e)
			case 'b':
				b = compat.AppendPlain(b, '\b')
			case 'f':
				b = compat.AppendPlain(b, '\f')
			case 'n':
				b = compat.AppendPlain(b, '\n')
			case 'r':
				b = compat.AppendPlain(b, '\r')
			case 't':
				b = compat.AppendPlain(b, '\t')
			case 'u':
				r, ok := p.hex4()
				if !ok {
					return "", p.invalid("in \\u hexadecimal character escape")
				}
				if 0xD800 <= r && r < 0xDC00 {
					// A surrogate pair, if a low surrogate follows.
					save := p.pos
					if p.pos+1 < len(p.data) && p.data[p.pos] == '\\' && p.data[p.pos+1] == 'u' {
						p.pos += 2
						if r2, ok := p.hex4(); ok && 0xDC00 <= r2 && r2 < 0xE000 {
							r = (r-0xD800)<<10 | (r2 - 0xDC00) + 0x10000
							b = compat.AppendRune(b, r)
							continue
						}
					}
					p.pos = save
					r = utf8.RuneError
				} else if 0xDC00 <= r && r < 0xE000 {
					r = utf8.RuneError
				}
				b = compat.AppendRune(b, r)
			default:
				p.pos--
				return "", p.invalid("in string escape code")
			}
		case c < utf8.RuneSelf:
			b = compat.AppendPlain(b, c)
			p.pos++
		default:
			r, size := utf8.DecodeRune(p.data[p.pos:])
			if r == utf8.RuneError && size == 1 {
				b = compat.AppendRune(b, utf8.RuneError)
			} else {
				b = compat.AppendPlain(b, p.data[p.pos:p.pos+size]...)
			}
			p.pos += size
		}
	}
}

func (p *jsonParser) hex4() (rune, bool) {
	if p.pos+4 > len(p.data) {
		return 0, false
	}
	var r rune
	for i := 0; i < 4; i++ {
		c := p.data[p.pos+i]
		switch {
		case '0' <= c && c <= '9':
			r = r<<4 | rune(c-'0')
		case 'a' <= c && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case 'A' <= c && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	p.pos += 4
	return r, true
}

// A decoder reads the parsed parameters of a message into the protocol's
// types, as json.Unmarshal would: unknown members are ignored, null
// leaves a field as it is, and the first value of the wrong type is the
// error (decoding goes on past it, like encoding/json's).
type decoder struct {
	err error
}

// mismatch records that v cannot be decoded as a Go value of type typ at
// the field path field ("" at the top).
func (d *decoder) mismatch(v *jsonValue, typ, field string) {
	if d.err != nil {
		return
	}
	what := v.kindName()
	if v.kind == jsonNumber {
		what = compat.Concat("number ", v.s)
	}
	if field == "" {
		d.err = compat.NewError(compat.Sprintfb("json: cannot unmarshal %s into Go value of type %s", compat.ArgS(what), compat.ArgS(typ)))
		return
	}
	d.err = compat.NewError(compat.Sprintfb("json: cannot unmarshal %s into Go struct field %s of type %s", compat.ArgS(what), compat.ArgS(field), compat.ArgS(typ)))
}

// object reports whether v is an object to decode into the struct type
// typ; null is not one, but no error either.
func (d *decoder) object(v *jsonValue, typ, field string) bool {
	if v.isNull() {
		return false
	}
	if v.kind != jsonObject {
		d.mismatch(v, typ, field)
		return false
	}
	return true
}

// array returns the elements of v, an array, or nil.
func (d *decoder) array(v *jsonValue, typ, field string) []*jsonValue {
	if v.isNull() {
		return nil
	}
	if v.kind != jsonArray {
		d.mismatch(v, typ, field)
		return nil
	}
	return v.elems
}

func (d *decoder) str(v *jsonValue, dst *string, field string) {
	if v.isNull() {
		return
	}
	if v.kind != jsonString {
		d.mismatch(v, "string", field)
		return
	}
	*dst = v.s
}

func (d *decoder) boolean(v *jsonValue, dst *bool, field string) {
	if v.isNull() {
		return
	}
	if v.kind != jsonBool {
		d.mismatch(v, "bool", field)
		return
	}
	*dst = v.b
}

func (d *decoder) integer(v *jsonValue, dst *int, field string) {
	if v.isNull() {
		return
	}
	if v.kind != jsonNumber {
		d.mismatch(v, "int", field)
		return
	}
	n, err := strconv.ParseInt(v.s, 10, 64)
	if err != nil {
		d.mismatch(v, "int", field)
		return
	}
	*dst = int(n)
}

// subfield joins a field path and a member name.
func subfield(field, name string) string {
	if field == "" {
		return name
	}
	return compat.Concat(field, ".", name)
}

// A jsonWriter writes JSON text as encoding/json.Marshal does.
type jsonWriter struct {
	buf   []byte
	comma bool // a value precedes, so the next one needs a comma
}

func newJSONWriter() *jsonWriter { return pu.New(jsonWriter{}) }

func (w *jsonWriter) bytes() []byte { return w.buf }

func (w *jsonWriter) sep() {
	if w.comma {
		w.buf = compat.AppendPlain(w.buf, ',')
	}
	w.comma = true
}

func (w *jsonWriter) beginObject() {
	w.sep()
	w.buf = compat.AppendPlain(w.buf, '{')
	w.comma = false
}

func (w *jsonWriter) endObject() {
	w.buf = compat.AppendPlain(w.buf, '}')
	w.comma = true
}

func (w *jsonWriter) beginArray() {
	w.sep()
	w.buf = compat.AppendPlain(w.buf, '[')
	w.comma = false
}

func (w *jsonWriter) endArray() {
	w.buf = compat.AppendPlain(w.buf, ']')
	w.comma = true
}

// key writes an object member's name; its value follows.
func (w *jsonWriter) key(name string) {
	w.sep()
	w.writeString(name)
	w.buf = compat.AppendPlain(w.buf, ':')
	w.comma = false
}

func (w *jsonWriter) null() {
	w.sep()
	w.buf = compat.AppendString(w.buf, "null")
}

func (w *jsonWriter) boolean(b bool) {
	w.sep()
	if b {
		w.buf = compat.AppendString(w.buf, "true")
	} else {
		w.buf = compat.AppendString(w.buf, "false")
	}
}

func (w *jsonWriter) integer(n int) {
	w.sep()
	w.buf = compat.AppendInt(w.buf, int64(n), 10)
}

func (w *jsonWriter) str(s string) {
	w.sep()
	w.writeString(s)
}

// raw writes a value that is already JSON text.
func (w *jsonWriter) raw(b []byte) {
	w.sep()
	w.buf = compat.AppendPlain(w.buf, b...)
}

const jsonHex = "0123456789abcdef"

// writeString writes a quoted string with encoding/json's escapes: <, >
// and & as \u escapes, U+2028 and U+2029 escaped, and invalid UTF-8 as
// U+FFFD.
func (w *jsonWriter) writeString(s string) {
	w.buf = compat.AppendPlain(w.buf, '"')
	for _, r := range s {
		switch {
		case r == '"':
			w.buf = compat.AppendString(w.buf, `\"`)
		case r == '\\':
			w.buf = compat.AppendString(w.buf, `\\`)
		case r == '\b':
			w.buf = compat.AppendString(w.buf, `\b`)
		case r == '\f':
			w.buf = compat.AppendString(w.buf, `\f`)
		case r == '\n':
			w.buf = compat.AppendString(w.buf, `\n`)
		case r == '\r':
			w.buf = compat.AppendString(w.buf, `\r`)
		case r == '\t':
			w.buf = compat.AppendString(w.buf, `\t`)
		case r < 0x20 || r == '<' || r == '>' || r == '&':
			w.buf = compat.AppendString(w.buf, `\u00`)
			w.buf = compat.AppendPlain(w.buf, jsonHex[r>>4], jsonHex[r&0xF])
		case r == 0x2028 || r == 0x2029:
			w.buf = compat.AppendString(w.buf, `\u202`)
			w.buf = compat.AppendPlain(w.buf, jsonHex[r&0xF])
		default:
			w.buf = compat.AppendRune(w.buf, r)
		}
	}
	w.buf = compat.AppendPlain(w.buf, '"')
}
