package main

import (
	"flag"
	"net"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/stesla/telnet"
	"golang.org/x/text/encoding/unicode"
)

var addr = flag.String("addr", getEnvDefault("ENVOY_ADDR", ":4001"), "address on which to listen")
var loglevel = flag.String("level", getEnvDefault("ENVOY_LOG_LEVEL", "info"), "log level")

var log = logrus.New()

func main() {
	flag.Parse()

	log.SetFormatter(new(logrus.TextFormatter))

	level, err := logrus.ParseLevel(*loglevel)
	if err != nil {
		log.Fatal(err)
	}
	log.SetLevel(level)

	log.Printf("envoy (pid %d) listening on '%s'", os.Getpid(), *addr)
	l, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}
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
}

func getEnvDefault(name, defaultValue string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return defaultValue
}

type logrusLogger struct {
	log *logrus.Logger
}

func (l logrusLogger) Logf(fmt string, args ...any) {
	l.log.Logf(logrus.DebugLevel, fmt, args...)
}
