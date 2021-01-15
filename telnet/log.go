package telnet

type Log interface {
	Debug(...interface{})
	Debugf(string, ...interface{})
	Trace(...interface{})
	Tracef(string, ...interface{})
}

type maybeLog struct {
	log Log
}

func (l *maybeLog) Debug(args ...interface{}) {
	if l.log != nil {
		l.log.Debug(args...)
	}
}

func (l *maybeLog) Debugf(fmt string, args ...interface{}) {
	if l.log != nil {
		l.log.Debugf(fmt, args...)
	}
}

func (l *maybeLog) Trace(args ...interface{}) {
	if l.log != nil {
		l.log.Trace(args...)
	}
}

func (l *maybeLog) Tracef(fmt string, args ...interface{}) {
	if l.log != nil {
		l.log.Tracef(fmt, args...)
	}
}
