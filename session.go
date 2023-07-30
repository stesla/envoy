package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/stesla/telnet"
	"golang.org/x/text/encoding/unicode"
)

type session struct {
	telnet.Conn
	*bufio.Scanner

	log      *logrusLogger
	password string
}

func newSession(client telnet.Conn, password string, log *logrusLogger) *session {
	session := &session{
		Conn:     client,
		password: password,
		log:      log,
	}
	session.Conn.SetLogger(session.log)
	session.Scanner = bufio.NewScanner(session)
	return session
}

func (s *session) negotiateOptions() {
	for _, opt := range []telnet.Option{
		telnet.NewSuppressGoAheadOption(),
		telnet.NewTransmitBinaryOption(),
		telnet.NewCharsetOption(true),
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
				return nil, errors.New("you must select a proxy to connect")
			}
			proxy.AddDownstream(s)
			if proxy.IsNew() {
				return proxy, proxy.Initialize(rest, buf.Bytes())
			} else {
				return proxy, proxy.WriteHistoryTo(s)
			}
		case "option":
			if proxy == nil {
				return nil, errors.New("you must select a proxy to set options")
			}
			option, value, _ := strings.Cut(rest, " ")
			if err := proxy.SetOption(option, value); err != nil {
				return nil, err
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
