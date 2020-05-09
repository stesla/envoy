package telnet

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"testing"
)

func processBytes(t *testing.T, b []byte) (r, w []byte) {
	return processBytesWithOptions(t, b, nil)
}

func processBytesWithOptions(t *testing.T, b []byte, opts map[byte]*option) (r, w []byte) {
	in := bytes.NewBuffer(b)
	out := &bytes.Buffer{}
	protocol := newTelnetProtocol(in, out)
	if opts != nil {
		protocol.options = opts
	}

	r = make([]byte, len(b)) // At most we'll read all the bytes
	if n, err := protocol.Read(r); err != nil {
		t.Fatalf("Read error %q", err)
	} else {
		r = r[0:n] // Truncate to the length actually read
		t.Logf("Read %d bytes %q", n, r)
	}
	w = out.Bytes()
	t.Logf("Wrote %d bytes %q", len(w), w)
	return
}

func assertEqual(t *testing.T, a, b []byte) {
	if !bytes.Equal(a, b) {
		t.Fatalf("Expected %q to be %q", a, b)
	}
}

/***
** Read
***/

func TestAsciiText(t *testing.T) {
	r, w := processBytes(t, []byte("hello"))
	assertEqual(t, r, []byte("hello"))
	assertEqual(t, w, []byte{})
}

func TestStripTelnetCommands(t *testing.T) {
	r, w := processBytes(t, []byte{'h', InterpretAsCommand, NoOperation, 'i'})
	assertEqual(t, r, []byte("hi"))
	assertEqual(t, w, []byte{})
}

func TestEscapedIAC(t *testing.T) {
	r, w := processBytes(t, []byte{'h', InterpretAsCommand, InterpretAsCommand, 'i'})
	assertEqual(t, r, []byte("h\xffi"))
	assertEqual(t, w, []byte{})
}

func TestSplitCommand(t *testing.T) {
	var in, out bytes.Buffer
	protocol := newTelnetProtocol(&in, &out)

	r := make([]byte, 2)
	in.Write([]byte{'h', InterpretAsCommand})
	n, _ := protocol.Read(r)
	assertEqual(t, r[:n], []byte("h"))
	in.Write([]byte{NoOperation, 'i'})
	n, _ = protocol.Read(r)
	assertEqual(t, r[:n], []byte("i"))
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
	assertEqual(t, buf[:n], []byte("AB"))
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
	assertEqual(t, out.Bytes(), expected)
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
	assertEqual(t, out.Bytes(), expected)
}

/***
** Option Negotiation
***/

func testOption(t *testing.T, command, response byte, message string, opts map[byte]*option) {
	t.Logf("testOption %s", message)
	r, w := processBytesWithOptions(t, []byte{'h', InterpretAsCommand, command, TransmitBinary, 'i'}, opts)
	assertEqual(t, r, []byte("hi"))
	assertEqual(t, w, []byte{InterpretAsCommand, response, TransmitBinary})
}

func TestNaiveOptionNegotiation(t *testing.T) {
	testOption(t, Do, Wont, "Do", nil)
	testOption(t, Will, Dont, "Will", nil)

	opts := map[byte]*option{
		TransmitBinary: &option{us: telnetQYes, them: telnetQYes},
	}
	testOption(t, Dont, Wont, "Dont", opts)
	testOption(t, Wont, Dont, "Wont", opts)
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
