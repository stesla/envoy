package telnet

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/encoding/unicode"
)

/***
** RFC 2066 -TELNET CHARSET Option
**/

func TestSubnegotiationRemovedFromStream(t *testing.T) {
	r, _, err := newDecodeTest([]byte{'h', IAC, SB, 'f', 'o', 'o', IAC, SE, 'i'}).decode()
	assert.NoError(t, err)
	assert.Equal(t, []byte("hi"), r)
}

func TestCharsetRejectedIfNotEnabled(t *testing.T) {
	in := []byte{'h', IAC, SB, Charset, charsetRequest}
	in = append(in, []byte(";UTF-8;US-ASCII")...)
	in = append(in, IAC, SE, 'i')
	r, w, err := newDecodeTest(in).decode()
	assert.NoError(t, err)
	assert.Equal(t, []byte("hi"), r)
	expected := []byte{IAC, SB, Charset, charsetRejected, IAC, SE}
	assert.Equal(t, expected, w)
}

func TestCharsetRejectedFromClientConnection(t *testing.T) {
	// Because it is not valid to reply to a CHARSET REQUEST message with
	// another CHARSET REQUEST message, if a CHARSET REQUEST message is
	// received after sending one, it means that both sides have sent them
	// simultaneously.  In this case, the server side MUST issue a negative
	// acknowledgment.  The client side MUST respond to the one from the
	// server. -- RFC 2066
	//
	// Because we negotiate this both directions, we can simply reject any
	// client REQUESTs (except as documented below).
	in := []byte{'h', IAC, SB, Charset, charsetRequest}
	in = append(in, []byte(";UTF-8;US-ASCII")...)
	in = append(in, IAC, SE, 'i')

	test := newDecodeTest(in)
	test.p.peerType = ClientType
	o := test.p.get(Charset)
	o.them = telnetQYes
	r, w, err := test.decode()
	assert.NoError(t, err)

	assert.Equal(t, []byte("hi"), r)

	expected := []byte{IAC, SB, Charset, charsetRejected, IAC, SE}
	assert.Equal(t, expected, w)
}

func TestCharsetAcceptedIfItWasSupposedToBeAnAccept(t *testing.T) {
	// It seems that BeipMU responds to the IAC SB CHARSET REQUEST ... IAC SE
	// with a response that _should_ be a IAC SB CHARSET ACCEPT ... IAC SE, but
	// uses REQUEST instead of ACCEPT. But it only sends a single character set
	// and no separator character. Which leads me to believe it's bugged.
	//
	// If we simply get "UTF-8"  as the charset list, we should treat it as
	// if it were an ACCEPT instead.
	in := []byte{'h', IAC, SB, Charset, charsetRequest}
	in = append(in, []byte("UTF-8")...)
	in = append(in, IAC, SE, 'i')

	test := newDecodeTest(in)
	test.p.peerType = ClientType
	o := test.p.get(Charset)
	o.them = telnetQYes
	r, w, err := test.decode()
	assert.NoError(t, err)

	assert.Equal(t, []byte("hi"), r)

	expected := []byte{IAC, WILL, TransmitBinary, IAC, DO, TransmitBinary}
	assert.Equal(t, expected, w)

	assert.Equal(t, unicode.UTF8, test.p.enc)
}

func TestCharsetAcceptAscii(t *testing.T) {
	in := []byte{'h', IAC, SB, Charset, charsetRequest}
	in = append(in, []byte("[TTABLE]\x01;ISO-8859-1;US-ASCII;CP437")...)
	in = append(in, IAC, SE, 'i')

	test := newDecodeTest(in)
	o := test.p.get(Charset)
	o.them = telnetQYes
	r, w, err := test.decode()
	assert.NoError(t, err)

	assert.Equal(t, []byte("hi"), r)

	expected := []byte{IAC, SB, Charset, charsetAccepted}
	expected = append(expected, []byte("US-ASCII")...)
	expected = append(expected, IAC, SE)
	assert.Equal(t, expected, w)
}

func TestCharsetAcceptUTF8(t *testing.T) {
	in := []byte{0x80, IAC, SB, Charset, charsetRequest}
	in = append(in, []byte("[TTABLE]\x01;UTF-8;ISO-8859-1;US-ASCII;CP437")...)
	in = append(in, IAC, SE, 0xe2, 0x80, 0xbb)

	test := newDecodeTest(in)
	o := test.p.get(Charset)
	o.them = telnetQYes
	r, w, err := test.decode()
	assert.NoError(t, err)

	assert.Equal(t, []byte("\x1a※"), r)

	expected := []byte{IAC, SB, Charset, charsetAccepted}
	expected = append(expected, []byte("UTF-8")...)
	expected = append(expected, IAC, SE)
	expected = append(expected, IAC, WILL, TransmitBinary)
	expected = append(expected, IAC, DO, TransmitBinary)
	assert.Equal(t, expected, w)

	assert.Equal(t, unicode.UTF8, test.p.enc)
}

func TestCharsetRequestOfUTF8Accepted(t *testing.T) {
	in := []byte{'h', IAC, SB, Charset, charsetAccepted}
	in = append(in, []byte("UTF-8")...)
	in = append(in, IAC, SE, 'i')

	test := newDecodeTest(in)
	o := test.p.get(Charset)
	o.them = telnetQYes
	r, w, err := test.decode()
	assert.NoError(t, err)

	assert.Equal(t, []byte("hi"), r)

	expected := []byte{IAC, WILL, TransmitBinary, IAC, DO, TransmitBinary}
	assert.Equal(t, expected, w)

	assert.Equal(t, unicode.UTF8, test.p.enc)
}

func TestBuffersWritesUntilCharsetFinished(t *testing.T) {
	var w bytes.Buffer
	p := newTelnetProtocol(ServerType, nil, &w)
	h := &CharsetOption{}
	h.Register(p)
	p.Write([]byte("※ hello "))
	p.SetEncoding(Raw)
	h.finishCharset(nil)
	p.Write([]byte("※ world ※"))
	assert.Equal(t, "※ hello ※ world ※", w.String())
}
