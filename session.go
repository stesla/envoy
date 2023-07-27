package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/stesla/telnet"
	"golang.org/x/text/encoding/unicode"
)

type session struct {
	telnet.Conn
	*bufio.Scanner

	log      *logrusLogger
	password string
}

func newSession(client telnet.Conn, password string) *session {
	session := &session{
		Conn:     client,
		password: password,
		log: newLogrusLogger(log, logrus.Fields{
			"type": "client",
			"peer": client.RemoteAddr().String(),
		}),
	}
	session.Conn.SetLogger(session.log)
	session.Scanner = bufio.NewScanner(session)
	return session
}

func (s *session) negotiateOptions() {
	for _, opt := range []telnet.Option{
		telnet.NewSuppressGoAheadOption(),
		telnet.NewTransmitBinaryOption(),
		telnet.NewCharsetOption(),
	} {
		opt.Allow(true, true)
		s.BindOption(opt)
	}

	s.AddListener("update-option", telnet.FuncListener{
		Func: func(data any) {
			switch t := data.(type) {
			case telnet.UpdateOptionEvent:
				switch opt := t.Option; opt.Byte() {
				case telnet.Charset:
					if t.WeChanged && opt.EnabledForUs() {
						s.RequestEncoding(unicode.UTF8)
					}
				}
			}
		},
	})

	s.EnableOptionForThem(telnet.SuppressGoAhead, true)
	s.EnableOptionForUs(telnet.SuppressGoAhead, true)

	s.EnableOptionForThem(telnet.TransmitBinary, true)
	s.EnableOptionForUs(telnet.TransmitBinary, true)

	s.EnableOptionForThem(telnet.Charset, true)
	s.EnableOptionForUs(telnet.Charset, true)
}

func (s *session) runForever() {
	if !s.isAuthenticated() {
		return
	}
	proxy, err := s.connectProxy()
	if err != nil {
		fmt.Fprintln(s, "error connecting upstream:", err)
		return
	}
	io.Copy(proxy, s)
}

func (s *session) connectProxy() (*Proxy, error) {
	var buf bytes.Buffer
	var proxy *Proxy
	for s.Scan() {
		switch command, rest, _ := strings.Cut(s.Text(), " "); command {
		case "connect":
			if proxy == nil {
				return nil, errors.New("must provide proxy command before connect command")
			}
			proxy.AddDownstream(s)
			if proxy.IsNew() {
				return proxy, proxy.Initialize(rest, buf.Bytes())
			} else {
				return proxy, proxy.WriteHistoryTo(s)
			}
		case "proxy":
			proxy = ProxyForKey(rest)
		case "send":
			fmt.Fprintln(&buf, rest)
		default:
			fmt.Fprintln(s, "unrecognized command: ", command)
		}
	}
	// the only case where we ever get here is if we fail to scan, which will
	// only happen if the client disconnected
	return nil, io.EOF
}

func (s *session) isAuthenticated() bool {
	if s.Scan() {
		return s.Text() == "login "+s.password
	}
	return false
}

func (s *session) Read(bytes []byte) (n int, err error) {
	return s.log.traceIO("Read", s.Conn.Read, bytes)
}

func (s *session) Write(bytes []byte) (n int, err error) {
	return s.log.traceIO("Write", s.Conn.Write, bytes)
}
