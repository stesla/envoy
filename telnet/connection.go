package telnet

import (
	"fmt"
	"io"
	"net"

	log "github.com/sirupsen/logrus"
)

type Conn interface {
	io.ReadWriteCloser
	Conn() net.Conn
	SetEncoding(Encoding)
	NegotiateOptions()
}

type connection struct {
	io.Reader
	io.Writer

	conn net.Conn
	p    *telnetProtocol
}

func Dial(addr string) (Conn, error) {
	conn, er := net.Dial("tcp", addr)
	if er != nil {
		return nil, er
	}
	fields := log.Fields{"type": "server", "addr": addr}
	return Wrap(fields, conn), nil
}

func Wrap(fields log.Fields, conn net.Conn) Conn {
	c := newConnection(fields, conn, conn)
	c.conn = conn
	return c
}

func newConnection(fields log.Fields, r io.Reader, w io.Writer) *connection {
	c := &connection{p: newTelnetProtocol(fields, r, w)}
	c.initializeOptions()
	c.SetEncoding(EncodingAscii)
	return c
}

func (c *connection) Close() error {
	return c.conn.Close()
}

func (c *connection) Conn() net.Conn {
	return c.conn
}

func (c *connection) SetEncoding(e Encoding) {
	switch e {
	case EncodingAscii:
		c.Reader = newAsciiDecoder(c.p)
		c.Writer = newAsciiEncoder(c.p)
	case EncodingUTF8:
		c.Reader, c.Writer = c.p, c.p
	default:
		panic("invalid encoding")
	}
}

func (c *connection) initializeOptions() {
	c.p.get(SuppressGoAhead).allow(true, true)
}

func (c *connection) NegotiateOptions() {
	c.p.get(SuppressGoAhead).enableThem()
	c.p.get(SuppressGoAhead).enableUs()
}

type invalidCodepointError byte

func (c invalidCodepointError) Error() string {
	return fmt.Sprintf("invalid codepoint for current encoding: %c", c)
}

type Encoding int

const (
	EncodingAscii Encoding = 0 + iota
	EncodingUTF8
)

type asciiDecoder struct {
	r io.Reader
}

func newAsciiDecoder(r io.Reader) io.Reader {
	return &asciiDecoder{r: r}
}

func (d *asciiDecoder) Read(out []byte) (int, error) {
	buf := make([]byte, len(out))
	nr, er := d.r.Read(buf)

	str := string(buf[:nr])
	n := 0
	for _, c := range str {
		if c < 128 {
			out[n] = byte(c)
		} else {
			out[n] = '?'
		}
		n++
	}
	return n, er
}

type asciiEncoder struct {
	w io.Writer
}

func newAsciiEncoder(w io.Writer) io.Writer {
	return &asciiEncoder{w}
}

func (e *asciiEncoder) Write(in []byte) (n int, err error) {
	out := make([]byte, len(in))
	for _, b := range in {
		if b > 127 {
			err = invalidCodepointError(b)
			break
		}
		out[n] = b
		n++
	}
	nw, ew := e.w.Write(out[:n])
	if ew != nil {
		return nw, ew
	}
	return nw, err
}
