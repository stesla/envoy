package telnet

import (
	"fmt"
	"io"
	"net"
)

type Conn interface {
	net.Conn
}

type Client interface {
	io.ReadWriteCloser
	SetEncoding(Encoding)
}

type connection struct {
	net.Conn
	c Client
}

func Dial(addr string) (Conn, error) {
	con, er := net.Dial("tcp", addr)
	if er != nil {
		return nil, er
	}
	return &connection{
		Conn: con,
		c:    NewClient(con, con),
	}, nil
}

func (c *connection) Read(b []byte) (int, error) {
	return c.c.Read(b)
}

func (c *connection) Write(b []byte) (int, error) {
	return c.c.Write(b)
}

func (c *connection) Close() error {
	if err := c.c.Close(); err != nil {
		c.Conn.Close()
		return err
	} else {
		return c.Conn.Close()
	}
}

type client struct {
	io.Reader
	io.Writer
	p *telnetProtocol
}

func NewClient(r io.Reader, w io.Writer) Client {
	c := &client{p: makeTelnetProtocol(r, w)}
	c.SetEncoding(EncodingAscii)
	return c
}

func (c *client) Close() error {
	return nil
}

func (c *client) SetEncoding(e Encoding) {
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
