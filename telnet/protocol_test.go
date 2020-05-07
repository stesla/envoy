package gotelnet

import (
	"bytes"
	"testing"
)

func processBytes(t *testing.T, b []byte) (r, w []byte) {
	in := bytes.NewBuffer(b)
	out := &bytes.Buffer{}
	protocol := makeTelnetProtocol(in, out)

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
	protocol := makeTelnetProtocol(&in, &out)

	r := make([]byte, 2)
	in.Write([]byte{'h', InterpretAsCommand})
	n, _ := protocol.Read(r)
	assertEqual(t, r[:n], []byte("h"))
	in.Write([]byte{NoOperation, 'i'})
	n, _ = protocol.Read(r)
	assertEqual(t, r[:n], []byte("i"))
}

func testOption(t *testing.T, command, response byte, message string) {
	t.Logf("testOption %s", message)
	r, w := processBytes(t, []byte{'h', InterpretAsCommand, command, 0, 'i'})
	assertEqual(t, r, []byte("hi"))
	assertEqual(t, w, []byte{InterpretAsCommand, response, 0})
}

func TestNaiveOptionNegotiation(t *testing.T) {
	testOption(t, Do, Wont, "Do")
	testOption(t, Dont, Wont, "Dont")
	testOption(t, Will, Dont, "Will")
	testOption(t, Wont, Dont, "Wont")
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
	protocol := makeTelnetProtocol(boomReader(2), &out)
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

func TestWriteAscii(t *testing.T) {
	var in, out bytes.Buffer
	protocol := makeTelnetProtocol(&in, &out)
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
	protocol := makeTelnetProtocol(&in, &out)
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
