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
	// TODO: more client stuff
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
	r io.Reader
	w io.Writer
}

func NewClient(r io.Reader, w io.Writer) Client {
	p := makeTelnetProtocol(r, w)
	return &client{p, p}
}

func (c *client) Close() error {
	return nil
}

func (c *client) Read(out []byte) (int, error) {
	in := make([]byte, len(out))
	nr, er := c.r.Read(in)
	if nr == 0 {
		return nr, er
	}
	str := string(in[:nr])
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

func (c *client) Write(in []byte) (n int, err error) {
	out := make([]byte, len(in))
	for _, b := range in {
		if b > 127 {
			err = invalidCodepointError(b)
			break
		}
		out[n] = b
		n++
	}
	nw, ew := c.w.Write(out[:n])
	if ew != nil {
		return nw, ew
	}
	return nw, err
}

type invalidCodepointError byte

func (c invalidCodepointError) Error() string {
	return fmt.Sprintf("invalid codepoint for current encoding: %c", c)
}
