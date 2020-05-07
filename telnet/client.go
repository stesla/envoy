package telnet

import (
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

func (c *client) Read(buf []byte) (int, error) {
	return c.r.Read(buf)
}

func (c *client) Write(buf []byte) (int, error) {
	return c.w.Write(buf)
}
