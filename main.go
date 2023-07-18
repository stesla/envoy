package main

import (
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/stesla/telnet"
)

var addr = flag.String("addr", getEnvDefault("ENVOY_ADDR", ":4001"), "address on which to listen")
var loglevel = flag.String("level", getEnvDefault("ENVOY_LOG_LEVEL", "info"), "log level")
var password = flag.String("password", os.Getenv("ENVOY_PASSWORD"), "password for server access")
var logdir = flag.String("logdir", getEnvDefault("ENVOY_LOG_DIR", "./logs"), "directory into which logs should be saved")

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

	signal.Ignore(os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)

	chReopenLogs := make(chan os.Signal, 1)
	signal.Notify(chReopenLogs, syscall.SIGHUP)
	go func() {
		for range chReopenLogs {
			log.Printf("reopening logs")
			ReopenLogFiles()
		}
	}()

	chExit := make(chan os.Signal, 1)
	signal.Notify(chExit, os.Interrupt, syscall.SIGTERM)
	go func() {
		for range chExit {
			sig := <-chExit
			log.Infof("received signal '%s', exiting", sig)
			CloseAll()
			os.Exit(0)
		}
	}()

	for {
		tcpconn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}
		conn := telnet.Server(tcpconn)
		conn.SetLogger(newLogrusLogger(log, logrus.Fields{
			"type": "client",
			"peer": conn.RemoteAddr().String(),
		}))
		go func() {
			defer conn.Close()
			log.Printf("%s connected", conn.RemoteAddr())
			session := newSession(conn, *password)
			session.negotiateOptions()
			session.runForever()
			log.Printf("%s disconnected", conn.RemoteAddr())
		}()
	}
}

func getEnvDefault(name, defaultValue string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return defaultValue
}
