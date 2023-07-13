package main

import (
	"flag"
	"net"
	"os"

	"github.com/sirupsen/logrus"
	"github.com/stesla/envoy/proxy"
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
		go proxy.StartSession(conn, log)
	}
}

func getEnvDefault(name, defaultValue string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return defaultValue
}
