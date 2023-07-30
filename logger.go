package main

import (
	"net"

	"github.com/sirupsen/logrus"
)

type logrusLogger struct {
	log    *logrus.Logger
	fields logrus.Fields
}

func newLogrusLogger(log *logrus.Logger, fields logrus.Fields) *logrusLogger {
	return &logrusLogger{
		log:    log,
		fields: fields,
	}
}

func (l *logrusLogger) Logf(fmt string, args ...any) {
	l.logEntry().Logf(logrus.DebugLevel, fmt, args...)
}

func (l *logrusLogger) logEntry() *logrus.Entry {
	return l.log.WithFields(l.fields)
}

func (l *logrusLogger) traceIO(name string, fn func([]byte) (int, error), buf []byte) (n int, err error) {
	entry := l.logEntry()
	n, err = fn(buf)
	if err != nil {
		entry = entry.WithError(err)
	}
	entry.Tracef("%s(%s)", name, buf[:n])
	return n, err
}

func (l *logrusLogger) traceConnection(conn net.Conn) net.Conn {
	return &traceConn{conn, l}
}

type traceConn struct {
	net.Conn
	log *logrusLogger
}

func (c *traceConn) Read(p []byte) (int, error) {
	return c.log.traceIO("Read", c.Conn.Read, p)
}

func (c *traceConn) Write(p []byte) (int, error) {
	return c.log.traceIO("Write", c.Conn.Write, p)
}
