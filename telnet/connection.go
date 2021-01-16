package telnet

import (
	"io"
	"net"
	"sync"

	"golang.org/x/text/encoding"
)

type PeerType string

const (
	ServerType PeerType = "server"
	ClientType PeerType = "client"
)

type Conn interface {
	io.ReadWriteCloser
	GetOption(byte) Option
	RegisterHandler(h OptionHandler)
	SetEncoding(encoding.Encoding)
	SetLog(Log)
	SetRawLogWriter(io.Writer)
}

type connection struct {
	conn net.Conn
	raw  *maybeWriter
	*telnetProtocol
}

func Wrap(peerType PeerType, conn net.Conn) Conn {
	c := newConnection(peerType, conn, conn)
	c.conn = conn
	return c
}

func newConnection(peerType PeerType, r io.Reader, w io.Writer) *connection {
	raw := &maybeWriter{}
	r = io.TeeReader(r, raw)
	c := &connection{raw: raw, telnetProtocol: newTelnetProtocol(peerType, r, w)}
	return c
}

func (c *connection) Close() error {
	return c.conn.Close()
}

func (c *connection) SetEncoding(enc encoding.Encoding) {
	c.setEncoding(enc)
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
