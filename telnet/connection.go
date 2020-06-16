package telnet

import (
	"io"
	"net"
	"sync"

	log "github.com/sirupsen/logrus"
	"golang.org/x/text/encoding"
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
	SetEncoding(encoding.Encoding)
	SetRawLogWriter(io.Writer)

	LogEntry() *log.Entry
}

type connection struct {
	conn net.Conn
	raw  *maybeWriter
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
	raw := &maybeWriter{}
	r = io.TeeReader(r, raw)
	c := &connection{raw: raw, telnetProtocol: newTelnetProtocol(fields, r, w)}
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

func (c *connection) SetEncoding(enc encoding.Encoding) {
	c.setEncoding(enc)
}

func (c *connection) initializeOptions() {
	c.telnetProtocol.get(Charset).allow(true, true)
	c.telnetProtocol.get(EndOfRecord).allow(true, true)
	c.telnetProtocol.get(SuppressGoAhead).allow(true, true)
	c.telnetProtocol.get(TransmitBinary).allow(true, true)
}

func (c *connection) NegotiateOptions() {
	switch c.telnetProtocol.ctype {
	case ClientType:
		c.telnetProtocol.get(EndOfRecord).enableThem()
		c.telnetProtocol.get(EndOfRecord).enableUs()
		c.telnetProtocol.get(SuppressGoAhead).enableThem()
		c.telnetProtocol.get(SuppressGoAhead).enableUs()
		fallthrough
	case ServerType:
		c.telnetProtocol.get(Charset).enableThem()
		c.telnetProtocol.get(Charset).enableUs()
	}
}

func (c *connection) SetRawLogWriter(w io.Writer) {
	c.raw.SetWriter(w)
}

type maybeWriter struct {
	w io.Writer
	sync.Mutex
}

func (m *maybeWriter) SetWriter(w io.Writer) {
	m.Lock()
	defer m.Unlock()
	m.w = w
}

func (m *maybeWriter) Write(p []byte) (int, error) {
	m.Lock()
	defer m.Unlock()
	if m.w == nil {
		return len(p), nil
	}
	return m.w.Write(p)
}
