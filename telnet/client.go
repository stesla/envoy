package gotelnet

import (
	"net"
)

type Conn interface {
	net.Conn
}

type client struct {
	net.Conn
	p *telnetProtocol
}

func Dial(addr string) (conn Conn, err error) {
	con, er := net.Dial("tcp", addr)
	if er != nil {
		return nil, er
	}
	p := makeTelnetProtocol(con, con)
	return &client{con, p}, nil
}

func (c client) Read(b []byte) (int, error) {
	return c.p.Read(b)
}

func (c client) Write(b []byte) (int, error) {
	return c.p.Write(b)
}
