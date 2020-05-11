package telnet

import (
	"io"
	"net"

	log "github.com/sirupsen/logrus"
)

type ConnType string

const (
	ServerType ConnType = "server"
	ClientType ConnType = "client"
)

type Conn interface {
	io.ReadWriteCloser
	Conn() net.Conn
	NegotiateOptions()

	LogEntry() *log.Entry
}

type connection struct {
	conn net.Conn
	*telnetProtocol
}

func Dial(addr string) (Conn, error) {
	conn, er := net.Dial("tcp", addr)
	if er != nil {
		return nil, er
	}
	fields := log.Fields{"type": ServerType, "addr": addr}
	return Wrap(fields, conn), nil
}

func Wrap(fields log.Fields, conn net.Conn) Conn {
	c := newConnection(fields, conn, conn)
	c.conn = conn
	return c
}

func newConnection(fields log.Fields, r io.Reader, w io.Writer) *connection {
	c := &connection{telnetProtocol: newTelnetProtocol(fields, r, w)}
	c.initializeOptions()
	return c
}

func (c *connection) Close() error {
	return c.conn.Close()
}

func (c *connection) Conn() net.Conn {
	return c.conn
}

func (c *connection) LogEntry() *log.Entry {
	return c.telnetProtocol.withFields()
}

func (c *connection) initializeOptions() {
	c.telnetProtocol.get(EndOfRecord).allow(true, true)
	c.telnetProtocol.get(SuppressGoAhead).allow(true, true)
}

func (c *connection) NegotiateOptions() {
	switch c.telnetProtocol.ctype {
	case ClientType:
		c.telnetProtocol.get(EndOfRecord).enableUs()
		c.telnetProtocol.get(EndOfRecord).enableThem()
		c.telnetProtocol.get(SuppressGoAhead).enableUs()
		c.telnetProtocol.get(SuppressGoAhead).enableThem()
	}
}
