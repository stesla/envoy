package proxy

import (
	"net"

	"github.com/sirupsen/logrus"
	"github.com/stesla/telnet"
	"golang.org/x/text/encoding/unicode"
)

func StartSession(conn net.Conn, log *logrus.Logger) {
	log.Printf("%s connected", conn.RemoteAddr())

	client := telnet.Client(conn)
	client.SetLogger(&logrusLogger{log})

	for _, opt := range []telnet.Option{
		telnet.NewSuppressGoAheadOption(),
		telnet.NewTransmitBinaryOption(),
		telnet.NewCharsetOption(),
	} {
		opt.Allow(true, true)
		client.BindOption(opt)
	}

	client.AddListener("update-option", telnet.FuncListener{
		Func: func(data any) {
			event, ok := data.(telnet.UpdateOptionEvent)
			if !ok {
				return
			}
			switch opt := event.Option; opt.Byte() {
			case telnet.Charset:
				if event.WeChanged && opt.EnabledForUs() {
					client.RequestEncoding(unicode.UTF8)
				}
			}
		},
	})

	client.EnableOptionForThem(telnet.SuppressGoAhead, true)
	client.EnableOptionForUs(telnet.SuppressGoAhead, true)

	client.EnableOptionForThem(telnet.TransmitBinary, true)
	client.EnableOptionForUs(telnet.TransmitBinary, true)

	client.EnableOptionForThem(telnet.Charset, true)
	client.EnableOptionForUs(telnet.Charset, true)

	for {
		buf := make([]byte, 1024)
		n, err := client.Read(buf)
		if err != nil {
			break
		}
		buf = buf[:n]
		buf = append([]byte("ECHO: "), buf...)
		_, err = client.Write(buf)
		if err != nil {
			break
		}
	}

	log.Printf("%s disconnected", conn.RemoteAddr())
}

type logrusLogger struct {
	log *logrus.Logger
}

func (l logrusLogger) Logf(fmt string, args ...any) {
	l.log.Logf(logrus.DebugLevel, fmt, args...)
}
