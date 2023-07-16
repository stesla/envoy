package main

import (
	"flag"
	"net"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/stesla/telnet"
)

var addr = flag.String("addr", getEnvDefault("ENVOY_ADDR", ":4001"), "address on which to listen")
var loglevel = flag.String("level", getEnvDefault("ENVOY_LOG_LEVEL", "info"), "log level")
var password = flag.String("password", os.Getenv("ENVOY_PASSWORD"), "password for server access")

var log = logrus.New()

func main() {
	flag.Parse()

	log.SetFormatter(new(logrus.TextFormatter))

	if *password == "" {
		log.Fatalln("must provide -password or set ENVOY_PASSWORD")
	}

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
		client := telnet.Server(conn)
		client.SetLogger(newLogrusLogger(log, logrus.Fields{
			"type": "client",
			"peer": client.RemoteAddr().String(),
		}))
		go func(client telnet.Conn) {
			defer client.Close()
			log.Printf("%s connected", client.RemoteAddr())
			session := newSession(client, *password)
			session.negotiateOptions()
			session.runForever()
			log.Printf("%s disconnected", client.RemoteAddr())
		}(client)
	}
}

func getEnvDefault(name, defaultValue string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return defaultValue
}
