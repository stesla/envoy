package telnet

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/encoding"
	"golang.org/x/text/transform"
)

type decodeTest struct {
	in  []byte
	out bytes.Buffer
	p   *telnetProtocol
}

func newDecodeTest(input []byte) (dt *decodeTest) {
	dt = &decodeTest{in: input}
	in := bytes.NewBuffer(dt.in)
	dt.p = newTelnetProtocol(testLogFields, in, &dt.out)
	return
}

func (t *decodeTest) decode() (r, w []byte, err error) {
	var buf bytes.Buffer
	_, er := buf.ReadFrom(t.p)
	if er != nil {
		err = fmt.Errorf("Read: %q", er)
	}
	r = buf.Bytes()
	if r == nil {
		r = []byte{}
	}
	w = t.out.Bytes()
	if w == nil {
		w = []byte{}
	}
	return
}

/***
** Read
***/

func TestAsciiText(t *testing.T) {
	r, _, err := newDecodeTest([]byte("hello")).decode()
	assert.NoError(t, err)
	assert.Equal(t, []byte("hello"), r)
}

func TestStripTelnetCommands(t *testing.T) {
	r, _, err := newDecodeTest([]byte{'h', IAC, NOP, 'i'}).decode()
	assert.NoError(t, err)
	assert.Equal(t, []byte("hi"), r)
}

func TestEscapedIAC(t *testing.T) {
	test := newDecodeTest([]byte{'h', IAC, IAC, 'i'})
	r, _, err := test.decode()
	assert.NoError(t, err)
	assert.Equal(t, []byte{'h', 0x1a, 'i'}, r)
}

func TestEscapedIACRaw(t *testing.T) {
	test := newDecodeTest([]byte{'h', IAC, IAC, 'i'})
	test.p.setEncoding(Raw)
	r, _, err := test.decode()
	assert.NoError(t, err)
	assert.Equal(t, []byte{'h', IAC, 'i'}, r)
}

func TestCRLFIsNewline(t *testing.T) {
	r, _, err := newDecodeTest([]byte("foo\r\nbar")).decode()
	assert.NoError(t, err)
	assert.Equal(t, []byte("foo\nbar"), r)
}

func TestCRNULIsCarriageReturn(t *testing.T) {
	r, _, err := newDecodeTest([]byte("foo\r\x00bar")).decode()
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
		r, _, err := newDecodeTest([]byte{'h', '\r', c, 'i'}).decode()
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

func sendBytes(in []byte, enc encoding.Encoding) []byte {
	var w bytes.Buffer
	p := newTelnetProtocol(testLogFields, nil, &w)
	p.setEncoding(enc)
	p.finishCharset(nil)
	p.Write(in)
	return w.Bytes()
}

func TestWriteAscii(t *testing.T) {
	actual := sendBytes([]byte("hello"), ASCII)
	assert.Equal(t, []byte("hello"), actual)
}

func TestWriteIAC(t *testing.T) {
	actual := sendBytes([]byte{'h', IAC, 'i'}, ASCII)
	assert.Equal(t, []byte{'h', 0x1a, 'i'}, actual)
}

func TestWriteIACRaw(t *testing.T) {
	actual := sendBytes([]byte{'h', IAC, 'i'}, Raw)
	assert.Equal(t, []byte{'h', IAC, IAC, 'i'}, actual)
}

func TestWriteNewline(t *testing.T) {
	actual := sendBytes([]byte("foo\nbar"), ASCII)
	assert.Equal(t, []byte("foo\r\nbar"), actual)
}

func TestWriteCarriageReturn(t *testing.T) {
	actual := sendBytes([]byte("foo\rbar"), ASCII)
	assert.Equal(t, []byte("foo\r\x00bar"), actual)
}

func TestBuffersWritesUntilCharsetFinished(t *testing.T) {
	var w bytes.Buffer
	p := newTelnetProtocol(testLogFields, nil, &w)
	p.Write([]byte("※ hello "))
	p.setEncoding(Raw)
	p.finishCharset(nil)
	p.Write([]byte("※ world ※"))
	assert.Equal(t, "※ hello ※ world ※", w.String())
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
