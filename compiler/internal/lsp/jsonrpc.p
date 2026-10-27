// Translated by go2pu from internal/lsp/jsonrpc.go; edited by hand since.
// The Go version reads with bufio and encodes with encoding/json; this one
// reads with its own buffered reader, which the server's loop also asks
// whether input is waiting, and encodes with json.p.

package lsp

import (
	"compiler/compat"
	"compiler/pu"
	"pluto/io"
	"pluto/strconv"
	"pluto/strings"
)

// The base protocol of LSP: JSON-RPC 2.0 messages, each preceded by a
// Content-Length header and a blank line.

// A message is any JSON-RPC message: a request (ID and Method), a
// notification (Method only) or a response (ID with Result or Error).
type message struct {
	ID        []byte // the id's JSON text; nil if there is none
	Method    string
	Params    *jsonValue // nil if there are none
	HasParams bool
	Error     *rpcError
}

// isRequest reports whether m expects a response.
func (m *message) isRequest() bool { return m.Method != "" && len(m.ID) > 0 }

// An rpcError is the error member of a response.
type rpcError struct {
	Code    int
	Message string
}

func (e *rpcError) Error() string {
	return compat.Sprintfb("%s (code %d)", compat.ArgS(e.Message), compat.ArgI(int64(e.Code)))
}

// JSON-RPC and LSP error codes.
const (
	codeParseError           = -32700
	codeInvalidRequest       = -32600
	codeMethodNotFound       = -32601
	codeInvalidParams        = -32602
	codeInternalError        = -32603
	codeServerNotInitialized = -32002
	codeRequestFailed        = -32803
)

func newError(code int, msg string) *rpcError {
	return pu.New(rpcError{Code: code, Message: msg})
}

// maxMessage bounds the size of one incoming message.
const maxMessage = 64 << 20

// A reader is a buffered reader of the input stream.
type reader struct {
	r   io.Reader
	buf []byte
	pos int // buf[pos:] is unread
	err error
}

func newReader(r io.Reader) *reader {
	return pu.New(reader{r: r, buf: compat.Make[byte](0)})
}

// buffered reports whether unread input is in the buffer.
func (r *reader) buffered() bool { return r.pos < len(r.buf) }

// fill reads more input into the buffer, and reports whether it got any.
func (r *reader) fill() bool {
	if r.err != nil {
		return false
	}
	if r.pos > 0 {
		n := copy(r.buf, r.buf[r.pos:])
		r.buf = r.buf[:n]
		r.pos = 0
	}
	var chunk [16384]byte
	n, err := r.r.Read(chunk[:])
	if n > 0 {
		r.buf = compat.Append(r.buf, chunk[:n]...)
	}
	if err != nil {
		r.err = err
	}
	return n > 0
}

// readLine reads up to and including a newline. Like bufio's ReadString
// it returns what it has with the error at the end of the input.
func (r *reader) readLine() ([]byte, error) {
	for {
		for i := r.pos; i < len(r.buf); i++ {
			if r.buf[i] == '\n' {
				line := compat.Clone(r.buf[r.pos : i+1])
				r.pos = i + 1
				return line, nil
			}
		}
		if len(r.buf)-r.pos > maxMessage {
			return nil, compat.NewError("header line too long")
		}
		if !r.fill() {
			line := compat.Clone(r.buf[r.pos:])
			r.pos = len(r.buf)
			err := r.err
			if err == nil {
				err = io.EOF
			}
			return line, err
		}
	}
}

// readFull reads exactly n bytes.
func (r *reader) readFull(n int) ([]byte, error) {
	for len(r.buf)-r.pos < n {
		if !r.fill() {
			err := r.err
			if err == io.EOF || err == nil {
				if r.pos < len(r.buf) {
					return nil, io.ErrUnexpectedEOF
				}
				return nil, io.EOF
			}
			return nil, err
		}
	}
	body := compat.Clone(r.buf[r.pos : r.pos+n])
	r.pos += n
	return body, nil
}

// readMessage reads one framed message.
func readMessage(r *reader) (*message, error) {
	length := -1
	for {
		raw, err := r.readLine()
		if err != nil {
			if err == io.EOF && len(raw) == 0 && length < 0 {
				return nil, io.EOF
			}
			return nil, compat.Wrap(compat.Concat("reading header: ", err.Error()), err)
		}
		line := strings.TrimRight(compat.String(raw), "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, compat.NewError(compat.Sprintfb("malformed header %q", compat.ArgS(line)))
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 || n > maxMessage {
				return nil, compat.NewError(compat.Sprintfb("bad Content-Length %q", compat.ArgS(value)))
			}
			length = n
		}
	}
	if length < 0 {
		return nil, compat.NewError("message without Content-Length")
	}
	body, err := r.readFull(length)
	if err != nil {
		return nil, compat.Wrap(compat.Concat("reading body: ", err.Error()), err)
	}
	m, err := decodeMessage(body)
	if err != nil {
		return pu.New(message{Error: newError(codeParseError, err.Error())}), nil
	}
	return m, nil
}

// decodeMessage decodes a message as json.Unmarshal decodes into the Go
// version's message struct.
func decodeMessage(body []byte) (*message, error) {
	v := parseJSON(body) or return
	m := pu.New(message{})
	var d decoder
	if v.isNull() {
		return m, nil
	}
	if v.kind != jsonObject {
		d.mismatch(v, "lsp.message", "")
		return nil, d.err
	}
	var jsonrpc string
	d.str(v.get("jsonrpc"), &jsonrpc, "message.jsonrpc")
	if id := v.get("id"); id != nil {
		m.ID = id.raw
	}
	d.str(v.get("method"), &m.Method, "message.method")
	if p := v.get("params"); p != nil {
		m.Params = p
		m.HasParams = true
	}
	if e := v.get("error"); d.object(e, "lsp.rpcError", "message.error") {
		rerr := pu.New(rpcError{})
		d.integer(e.get("code"), &rerr.Code, "message.error.code")
		d.str(e.get("message"), &rerr.Message, "message.error.message")
		m.Error = rerr
	}
	if d.err != nil {
		return nil, d.err
	}
	return m, nil
}

// A writer sends framed messages.
type writer struct {
	w io.Writer
}

func (w *writer) send(body []byte) error {
	header := compat.Sprintfb("Content-Length: %d\r\n\r\n", compat.ArgI(int64(len(body))))
	if _, err := w.w.Write(compat.Bytes(header)); err != nil {
		return err
	}
	_, err := w.w.Write(body)
	return err
}

// reply sends a response: result, the JSON text of the result (nil for
// null), or rerr.
func (w *writer) reply(id []byte, result []byte, rerr *rpcError) error {
	j := newJSONWriter()
	j.beginObject()
	j.key("jsonrpc")
	j.str("2.0")
	j.key("id")
	if id == nil {
		j.null()
	} else {
		j.raw(compactJSON(id))
	}
	if rerr != nil {
		j.key("error")
		j.beginObject()
		j.key("code")
		j.integer(rerr.Code)
		j.key("message")
		j.str(rerr.Message)
		j.endObject()
	} else {
		j.key("result")
		if result == nil {
			j.null()
		} else {
			j.raw(result)
		}
	}
	j.endObject()
	return w.send(j.bytes())
}

// notify sends a notification; params is the JSON text of its parameters.
func (w *writer) notify(method string, params []byte) error {
	j := newJSONWriter()
	j.beginObject()
	j.key("jsonrpc")
	j.str("2.0")
	j.key("method")
	j.str(method)
	j.key("params")
	j.raw(params)
	j.endObject()
	return w.send(j.bytes())
}

// compactJSON removes the insignificant space from a JSON text and
// escapes <, >, & and U+2028 and U+2029 in its strings, as encoding/json
// does to a RawMessage it encodes.
func compactJSON(b []byte) []byte {
	out := compat.MakeCap[byte](0, len(b))
	inString := false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inString {
			switch {
			case c == '<' || c == '>' || c == '&':
				out = compat.AppendString(out, "\\u00")
				out = compat.Append(out, jsonHex[c>>4], jsonHex[c&0xf])
				continue
			case c == 0xe2 && i+2 < len(b) && b[i+1] == 0x80 && (b[i+2] == 0xa8 || b[i+2] == 0xa9):
				out = compat.AppendString(out, "\\u202")
				out = compat.Append(out, jsonHex[b[i+2]&0xf])
				i += 2
				continue
			}
			out = compat.Append(out, c)
			if c == '\\' && i+1 < len(b) {
				i++
				out = compat.Append(out, b[i])
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		case '"':
			inString = true
		}
		out = compat.Append(out, c)
	}
	return out
}
