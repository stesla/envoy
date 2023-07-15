package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/stesla/telnet"
	"golang.org/x/text/encoding/unicode"
)

type session struct {
	telnet.Conn
	*bufio.Scanner

	password string
}

func newSession(client telnet.Conn, password string) *session {
	session := &session{
		Conn:     client,
		password: password,
	}
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

func (s *session) connectProxy() (Proxy, error) {
	var buf bytes.Buffer
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "connect ") {
			args := strings.Fields(strings.TrimPrefix(line, "connect "))
			if len(args) != 2 {
				fmt.Fprintln(s, "USAGE: connect KEY ADDR")
				continue
			}
			key, addr := args[0], args[1]
			fmt.Fprintln(s, "connect", key, addr, fmt.Sprintf("%q", buf.String()))
			return ConnectProxy(key, s, addr, buf.Bytes())
		} else if strings.HasPrefix(line, "send ") {
			line = strings.TrimPrefix(line, "send ")
			fmt.Fprintln(&buf, line)
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
