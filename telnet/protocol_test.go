package telnet

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func processBytes(b []byte) (r, w []byte, _ error) {
	return processBytesWithOptions(b, nil)
}

func processBytesWithOptions(b []byte, opts map[byte]*option) (r, w []byte, err error) {
	in := bytes.NewBuffer(b)
	var out bytes.Buffer
	protocol := newTelnetProtocol(in, &out)
	if opts != nil {
		protocol.options = opts
	}

	r = make([]byte, len(b)) // At most we'll read all the bytes
	nr, er := protocol.Read(r)
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
	r, w, err := processBytes([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("hello"), r)
	assert.Equal(t, []byte{}, w)
}

func TestStripTelnetCommands(t *testing.T) {
	r, w, err := processBytes([]byte{'h', InterpretAsCommand, NoOperation, 'i'})
	assert.NoError(t, err)
	assert.Equal(t, []byte("hi"), r)
	assert.Equal(t, []byte{}, w)
}

func TestEscapedIAC(t *testing.T) {
	r, w, err := processBytes([]byte{'h', InterpretAsCommand, InterpretAsCommand, 'i'})
	assert.NoError(t, err)
	assert.Equal(t, []byte("h\xffi"), r)
	assert.Equal(t, []byte{}, w)
}

func TestSplitCommand(t *testing.T) {
	var in, out bytes.Buffer
	protocol := newTelnetProtocol(&in, &out)

	r := make([]byte, 2)
	in.Write([]byte{'h', InterpretAsCommand})
	n, _ := protocol.Read(r)
	assert.Equal(t, []byte("h"), r[:n])
	in.Write([]byte{NoOperation, 'i'})
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
	protocol := newTelnetProtocol(boomReader(2), &out)
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

func TestWriteAscii(t *testing.T) {
	var in, out bytes.Buffer
	protocol := newTelnetProtocol(&in, &out)
	expected := []byte("hello")
	n, err := protocol.Write(expected)
	if err != nil {
		t.Fatalf("Error Writing: %q", err)
	}
	if n != len(expected) {
		t.Fatalf("Expected to write %d but wrote %d", len(expected), n)
	}
	assert.Equal(t, expected, out.Bytes())
}

func TestWriteIAC(t *testing.T) {
	var in, out bytes.Buffer
	protocol := newTelnetProtocol(&in, &out)
	n, err := protocol.Write([]byte{'h', InterpretAsCommand, 'i'})
	if err != nil {
		t.Fatalf("Error Writing: %q", err)
	}
	if n != 3 {
		t.Fatalf("Expected to write 3 but wrote %d", n)
	}
	expected := []byte{'h', InterpretAsCommand, InterpretAsCommand, 'i'}
	assert.Equal(t, expected, out.Bytes())
}

/***
** Option Negotiation
***/

func TestDoNotSupportEcho(t *testing.T) {
	tests := []struct {
		command, response      byte
		message                string
		usEnabled, themEnabled bool
	}{
		{Will, Dont, "Will", false, false},
		{Do, Wont, "Do", false, false},
		{Wont, Dont, "Wont", false, true},
		// This case will never happen, since we don't support the
		// option and would never enable it, but to thoroughly test the
		// option negotiation, I am adding it for completeness. What
		// will _actually_ happen if we receive IAC DONT ECHO is
		// absolutely nothing, because we already have it disabled, so
		// we'll ignore it per the Q Method (RFC 1143).
		{Dont, Wont, "Dont", true, false},
	}
	for _, test := range tests {
		t.Logf("testOption %s", test.message)
		o := &option{code: Echo}
		if test.usEnabled {
			o.us = telnetQYes
		}
		if test.themEnabled {
			o.them = telnetQYes
		}
		opts := map[byte]*option{Echo: o}
		r, w, err := processBytesWithOptions([]byte{'h', InterpretAsCommand, test.command, Echo, 'i'}, opts)
		assert.NoError(t, err)
		assert.Equal(t, []byte("hi"), r)
		assert.Equal(t, []byte{InterpretAsCommand, test.response, Echo}, w)
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

func TestQMethodReceiveDo(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, permitted: false, end: telnetQNo, expected: Wont},
		&qMethodTest{start: telnetQNo, permitted: true, end: telnetQYes, expected: Will},
		&qMethodTest{start: telnetQYes, end: telnetQYes},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQYes},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQYes},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantNoEmpty, expected: Wont},
	}
	for _, q := range tests {
		o := &option{code: SuppressGoAhead, us: q.start, allowUs: q.permitted}
		o.receive(q, Do)
		assert.Equalf(t, q.end, o.us, "expected %s got %s", q.end, o.us)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodReceiveDont(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQNo},
		&qMethodTest{start: telnetQYes, end: telnetQNo, expected: Wont},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantYesEmpty, expected: Will},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQNo},
	}
	for _, q := range tests {
		o := &option{code: SuppressGoAhead, us: q.start, allowThem: q.permitted}
		o.receive(q, Dont)
		assert.Equalf(t, q.end, o.us, "expected %s got %s", q.end, o.us)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodReceiveWill(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, permitted: false, end: telnetQNo, expected: Dont},
		&qMethodTest{start: telnetQNo, permitted: true, end: telnetQYes, expected: Do},
		&qMethodTest{start: telnetQYes, end: telnetQYes},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQYes},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQYes},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantNoEmpty, expected: Dont},
	}
	for _, q := range tests {
		o := &option{code: SuppressGoAhead, them: q.start, allowThem: q.permitted}
		o.receive(q, Will)
		assert.Equalf(t, q.end, o.them, "expected %s got %s", q.end, o.them)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodReceiveWont(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQNo},
		&qMethodTest{start: telnetQYes, end: telnetQNo, expected: Dont},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantYesEmpty, expected: Do},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQNo},
	}
	for _, q := range tests {
		o := &option{code: SuppressGoAhead, them: q.start, allowThem: q.permitted}
		o.receive(q, Wont)
		assert.Equalf(t, q.end, o.them, "expected %s got %s", q.end, o.them)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodAskEnableThem(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQWantYesEmpty, expected: Do},
		&qMethodTest{start: telnetQYes, end: telnetQYes},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQWantNoOpposite},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantNoOpposite},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQWantYesEmpty},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantYesEmpty},
	}
	for _, q := range tests {
		o := &option{code: SuppressGoAhead, them: q.start}
		o.enableThem(q)
		assert.Equalf(t, q.end, o.them, "expected %s got %s", q.end, o.them)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodDisableThem(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQNo},
		&qMethodTest{start: telnetQYes, end: telnetQWantNoEmpty, expected: Dont},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQWantNoEmpty},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantNoEmpty},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQWantYesOpposite},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantYesOpposite},
	}
	for _, q := range tests {
		o := &option{code: SuppressGoAhead, them: q.start}
		o.disableThem(q)
		assert.Equalf(t, q.end, o.them, "expected %s got %s", q.end, o.them)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodEnableUs(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQWantYesEmpty, expected: Will},
		&qMethodTest{start: telnetQYes, end: telnetQYes},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQWantNoOpposite},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantNoOpposite},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQWantYesEmpty},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantYesEmpty},
	}
	for _, q := range tests {
		o := &option{code: SuppressGoAhead, us: q.start}
		o.enableUs(q)
		assert.Equalf(t, q.end, o.us, "expected %s got %s", q.end, o.us)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodDisableUs(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQNo},
		&qMethodTest{start: telnetQYes, end: telnetQWantNoEmpty, expected: Wont},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQWantNoEmpty},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantNoEmpty},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQWantYesOpposite},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantYesOpposite},
	}
	for _, q := range tests {
		o := &option{code: SuppressGoAhead, us: q.start}
		o.disableUs(q)
		assert.Equalf(t, q.end, o.us, "expected %s got %s", q.end, o.us)
		if q.expected != 0 {
			assert.Equal(t, []byte{q.expected, SuppressGoAhead}, q.actual)
		}
	}
}
