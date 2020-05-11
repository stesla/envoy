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
** Option Negotiation
***/

func TestDONotSupportEcho(t *testing.T) {
	tests := []struct {
		command, response      byte
		message                string
		usEnabled, themEnabled bool
	}{
		{WILL, DONT, "WILL", false, false},
		{DO, WONT, "DO", false, false},
		{WONT, DONT, "WONT", false, true},
		// This case will never happen, since we don't support the
		// option and would never enable it, but to thoroughly test the
		// option negotiation, I am adding it for completeness. What
		// will _actually_ happen if we receive IAC DONT ECHO is
		// absolutely nothing, because we already have it disabled, so
		// we'll ignore it per the Q Method (RFC 1143).
		{DONT, WONT, "DONT", true, false},
	}
	for _, test := range tests {
		t.Logf("testOption %s", test.message)
		opts := newOptionMap(nil)
		o := opts.get(Echo)
		if test.usEnabled {
			o.us = telnetQYes
		}
		if test.themEnabled {
			o.them = telnetQYes
		}
		r, w, err := processBytesWithOptions([]byte{'h', IAC, test.command, Echo, 'i'}, opts)
		assert.NoError(t, err)
		assert.Equal(t, []byte("hi"), r)
		assert.Equal(t, []byte{IAC, test.response, Echo}, w)
	}
}

type qMethodTest struct {
	start, end telnetQState
	permitted  bool
	expected   byte
	actual     []byte
}

func (q *qMethodTest) sendCommand(actual ...byte) error {
	q.actual = actual
	return nil
}

func TestQMethodReceiveDO(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, permitted: false, end: telnetQNo, expected: WONT},
		&qMethodTest{start: telnetQNo, permitted: true, end: telnetQYes, expected: WILL},
		&qMethodTest{start: telnetQYes, end: telnetQYes},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQYes},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQYes},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantNoEmpty, expected: WONT},
	}
	for _, q := range tests {
		o := &option{cs: q, code: SuppressGoAhead, us: q.start, allowUs: q.permitted}
		o.receive(DO)
		assert.Equalf(t, q.end, o.us, "expected %s got %s", q.end, o.us)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodReceiveDONT(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQNo},
		&qMethodTest{start: telnetQYes, end: telnetQNo, expected: WONT},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantYesEmpty, expected: WILL},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQNo},
	}
	for _, q := range tests {
		o := &option{cs: q, code: SuppressGoAhead, us: q.start, allowThem: q.permitted}
		o.receive(DONT)
		assert.Equalf(t, q.end, o.us, "expected %s got %s", q.end, o.us)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodReceiveWILL(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, permitted: false, end: telnetQNo, expected: DONT},
		&qMethodTest{start: telnetQNo, permitted: true, end: telnetQYes, expected: DO},
		&qMethodTest{start: telnetQYes, end: telnetQYes},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQYes},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQYes},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantNoEmpty, expected: DONT},
	}
	for _, q := range tests {
		o := &option{cs: q, code: SuppressGoAhead, them: q.start, allowThem: q.permitted}
		o.receive(WILL)
		assert.Equalf(t, q.end, o.them, "expected %s got %s", q.end, o.them)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodReceiveWONT(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQNo},
		&qMethodTest{start: telnetQYes, end: telnetQNo, expected: DONT},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantYesEmpty, expected: DO},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQNo},
	}
	for _, q := range tests {
		o := &option{cs: q, code: SuppressGoAhead, them: q.start, allowThem: q.permitted}
		o.receive(WONT)
		assert.Equalf(t, q.end, o.them, "expected %s got %s", q.end, o.them)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodAskEnableThem(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQWantYesEmpty, expected: DO},
		&qMethodTest{start: telnetQYes, end: telnetQYes},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQWantNoOpposite},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantNoOpposite},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQWantYesEmpty},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantYesEmpty},
	}
	for _, q := range tests {
		o := &option{cs: q, code: SuppressGoAhead, them: q.start}
		o.enableThem()
		assert.Equalf(t, q.end, o.them, "expected %s got %s", q.end, o.them)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodDisableThem(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQNo},
		&qMethodTest{start: telnetQYes, end: telnetQWantNoEmpty, expected: DONT},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQWantNoEmpty},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantNoEmpty},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQWantYesOpposite},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantYesOpposite},
	}
	for _, q := range tests {
		o := &option{cs: q, code: SuppressGoAhead, them: q.start}
		o.disableThem()
		assert.Equalf(t, q.end, o.them, "expected %s got %s", q.end, o.them)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodEnableUs(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQWantYesEmpty, expected: WILL},
		&qMethodTest{start: telnetQYes, end: telnetQYes},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQWantNoOpposite},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantNoOpposite},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQWantYesEmpty},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantYesEmpty},
	}
	for _, q := range tests {
		o := &option{cs: q, code: SuppressGoAhead, us: q.start}
		o.enableUs()
		assert.Equalf(t, q.end, o.us, "expected %s got %s", q.end, o.us)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodDisableUs(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQNo},
		&qMethodTest{start: telnetQYes, end: telnetQWantNoEmpty, expected: WONT},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQWantNoEmpty},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantNoEmpty},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQWantYesOpposite},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantYesOpposite},
	}
	for _, q := range tests {
		o := &option{cs: q, code: SuppressGoAhead, us: q.start}
		o.disableUs()
		assert.Equalf(t, q.end, o.us, "expected %s got %s", q.end, o.us)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
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
