package main

import (
	"github.com/stesla/telnet"
	"golang.org/x/text/encoding/unicode"
)

type session struct {
	client telnet.Conn
}

func newSession(client telnet.Conn) *session {
	return &session{client}
}

func (s *session) negotiateOptions() {
	for _, opt := range []telnet.Option{
		telnet.NewSuppressGoAheadOption(),
		telnet.NewTransmitBinaryOption(),
		telnet.NewCharsetOption(),
	} {
		opt.Allow(true, true)
		s.client.BindOption(opt)
	}

	s.client.AddListener("update-option", telnet.FuncListener{
		Func: func(data any) {
			event, ok := data.(telnet.UpdateOptionEvent)
			if !ok {
				return
			}
			switch opt := event.Option; opt.Byte() {
			case telnet.Charset:
				if event.WeChanged && opt.EnabledForUs() {
					s.client.RequestEncoding(unicode.UTF8)
				}
			}
		},
	})

	s.client.EnableOptionForThem(telnet.SuppressGoAhead, true)
	s.client.EnableOptionForUs(telnet.SuppressGoAhead, true)

	s.client.EnableOptionForThem(telnet.TransmitBinary, true)
	s.client.EnableOptionForUs(telnet.TransmitBinary, true)

	s.client.EnableOptionForThem(telnet.Charset, true)
	s.client.EnableOptionForUs(telnet.Charset, true)
}

func (s *session) runForever() {
	for {
		buf := make([]byte, 1024)
		n, err := s.client.Read(buf)
		if err != nil {
			break
		}
		buf = buf[:n]
		buf = append([]byte("ECHO: "), buf...)
		_, err = s.client.Write(buf)
		if err != nil {
			break
		}
	}
}
