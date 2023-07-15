package main

import (
	"github.com/stesla/telnet"
	"golang.org/x/text/encoding/unicode"
)

type session struct {
	telnet.Conn
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
		s.BindOption(opt)
	}

	s.AddListener("update-option", telnet.FuncListener{
		Func: func(data any) {
			event, ok := data.(telnet.UpdateOptionEvent)
			if !ok {
				return
			}
			switch opt := event.Option; opt.Byte() {
			case telnet.Charset:
				if event.WeChanged && opt.EnabledForUs() {
					s.RequestEncoding(unicode.UTF8)
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
	for {
		buf := make([]byte, 1024)
		n, err := s.Read(buf)
		if err != nil {
			break
		}
		buf = buf[:n]
		buf = append([]byte("ECHO: "), buf...)
		_, err = s.Write(buf)
		if err != nil {
			break
		}
	}
}
