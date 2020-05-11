package telnet

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/encoding"
	"golang.org/x/text/transform"
)

func processBytes(b []byte) (r, w []byte, _ error) {
	return processBytesWithOptions(b, newOptionMap(nil))
}

func processBytesWithOptions(b []byte, opts *optionMap) (r, w []byte, err error) {
	in := bytes.NewBuffer(b)
	var out bytes.Buffer
	p := newTelnetProtocol(testLogFields, in, &out)
	p.setEncoding(Raw)
	p.optionMap.merge(opts)

	r = make([]byte, len(b)) // At most we'll read all the bytes
	nr, er := p.Read(r)
	if er != nil {
		err = fmt.Errorf("Read: %q", er)
	}
	r = r[0:nr] // Truncate to the length actually read
	w = out.Bytes()
	if w == nil {
		w = []byte{}
	}
	return
}

/***
** Read
***/

func TestAsciiText(t *testing.T) {
	r, _, err := processBytes([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("hello"), r)
}

func TestStripTelnetCommands(t *testing.T) {
	r, _, err := processBytes([]byte{'h', IAC, NOP, 'i'})
	assert.NoError(t, err)
	assert.Equal(t, []byte("hi"), r)
}

func TestEscapedIAC(t *testing.T) {
	r, _, err := processBytes([]byte{'h', IAC, IAC, 'i'})
	assert.NoError(t, err)
	assert.Equal(t, []byte{'h', IAC, 'i'}, r)
}

func TestCRLFIsNewline(t *testing.T) {
	r, _, err := processBytes([]byte("foo\r\nbar"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("foo\nbar"), r)
}

func TestCRNULIsCarriageReturn(t *testing.T) {
	r, _, err := processBytes([]byte("foo\r\x00bar"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("foo\rbar"), r)
}

func TestCRIsOtherwiseIgnored(t *testing.T) {
	const (
		minByte byte = 0
		maxByte      = 127
	)
	for c := minByte; c < maxByte; c++ {
		if c == '\x00' || c == '\n' {
			continue
		}
		r, _, err := processBytes([]byte{'h', '\r', c, 'i'})
		assert.NoError(t, err)
		assert.Equal(t, []byte{'h', c, 'i'}, r)
	}
}

func TestSplitCommand(t *testing.T) {
	var in, out bytes.Buffer
	protocol := newTelnetProtocol(testLogFields, &in, &out)

	r := make([]byte, 2)
	in.Write([]byte{'h', IAC})
	n, _ := protocol.Read(r)
	assert.Equal(t, []byte("h"), r[:n])
	in.Write([]byte{NOP, 'i'})
	n, _ = protocol.Read(r)
	assert.Equal(t, []byte("i"), r[:n])
}

type Error string

func (e Error) Error() string { return string(e) }

type boomReader int

func (r boomReader) Read(b []byte) (n int, err error) {
	for i := 0; i < int(r); i++ {
		b[i] = 'A' + byte(i)
	}
	return int(r), Error("boom")
}

func TestErrorReading(t *testing.T) {
	var out bytes.Buffer
	protocol := newTelnetProtocol(testLogFields, boomReader(2), &out)
	buf := make([]byte, 16)
	n, err := protocol.Read(buf)
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Error() != "boom" {
		t.Fatalf("expected \"boom\", got %q", err)
	}
	assert.Equal(t, []byte("AB"), buf[:n])
}

/***
** Write
***/

func sendBytes(in []byte) []byte {
	var r, w bytes.Buffer
	p := newTelnetProtocol(testLogFields, &r, &w)
	p.setEncoding(Raw)
	p.Write(in)
	return w.Bytes()
}

func TestWriteAscii(t *testing.T) {
	actual := sendBytes([]byte("hello"))
	assert.Equal(t, []byte("hello"), actual)
}

func TestWriteIAC(t *testing.T) {
	actual := sendBytes([]byte{'h', IAC, 'i'})
	assert.Equal(t, []byte{'h', IAC, IAC, 'i'}, actual)
}

func TestWriteNewline(t *testing.T) {
	actual := sendBytes([]byte("foo\nbar"))
	assert.Equal(t, []byte("foo\r\nbar"), actual)
}

func TestWriteCarriageReturn(t *testing.T) {
	actual := sendBytes([]byte("foo\rbar"))
	assert.Equal(t, []byte("foo\r\x00bar"), actual)
}

/***
** Raw Encoding
***/

var Raw encoding.Encoding = &rawEncoding{}

type rawEncoding struct{}

func (r *rawEncoding) NewDecoder() *encoding.Decoder {
	return &encoding.Decoder{Transformer: r}
}

func (r *rawEncoding) NewEncoder() *encoding.Encoder {
	return &encoding.Encoder{Transformer: r}
}

func (r *rawEncoding) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	n := copy(dst, src)
	nDst, nSrc = n, n
	if nSrc < len(src) {
		err = transform.ErrShortDst
	}
	return
}

func (r *rawEncoding) Reset() {}
