package main

import "github.com/sirupsen/logrus"

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

func (l logrusLogger) Logf(fmt string, args ...any) {
	l.log.WithFields(l.fields).Logf(logrus.DebugLevel, fmt, args...)
}
