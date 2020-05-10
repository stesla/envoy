package telnet

import (
	"io"

	log "github.com/sirupsen/logrus"
)

type telnetProtocol struct {
	fields log.Fields
	in     io.Reader
	out    io.Writer
	state  readerState

	*optionMap
}

func newTelnetProtocol(fields log.Fields, r io.Reader, w io.Writer) *telnetProtocol {
	p := &telnetProtocol{
		fields: fields,
		in:     r,
		out:    w,
		state:  readAscii,
	}
	p.optionMap = newOptionMap(p)
	return p
}

func (p *telnetProtocol) withFields() *log.Entry {
	return log.WithFields(p.fields)
}

func (p *telnetProtocol) Read(b []byte) (n int, err error) {
	buf := make([]byte, len(b))
	nr, err := p.in.Read(buf)
	buf = buf[:nr]
	for len(buf) > 0 && n < len(b) {
		var ok bool
		var c byte
		p.state, c, ok = p.state(p, buf[0])
		if ok {
			b[n] = c
			n++
		}
		buf = buf[1:]
	}
	return n, err
}

func (p *telnetProtocol) Write(b []byte) (n int, err error) {
	for n = 0; len(b) > 0 && err == nil; n++ {
		switch b[0] {
		case IAC:
			err = p.sendCommand(IAC)
		case '\n':
			_, err = p.out.Write([]byte("\r\n"))
		case '\r':
			_, err = p.out.Write([]byte("\r\x00"))
		default:
			_, err = p.out.Write(b[0:1])
		}
		b = b[1:]
	}
	return
}

func (p *telnetProtocol) sendCommand(cmd ...byte) (err error) {
	cmd = append([]byte{IAC}, cmd...)
	if log.IsLevelEnabled(log.DebugLevel) {
		str := "SENT"
		for _, c := range cmd {
			str += " " + command(c).String()
		}
		p.withFields().Debug(str)
	}
	_, err = p.out.Write(cmd)
	return
}

type readerState func(*telnetProtocol, byte) (readerState, byte, bool)

func readAscii(_ *telnetProtocol, c byte) (readerState, byte, bool) {
	switch c {
	case IAC:
		return readCommand, c, false
	case '\r':
		return readCR, c, false
	}
	return readAscii, c, true
}

func readCommand(p *telnetProtocol, c byte) (readerState, byte, bool) {
	switch c {
	case IAC:
		p.withFields().Debug("RECV IAC IAC")
		return readAscii, c, true
	case DO, DONT, WILL, WONT:
		return readOption(c), c, false
	}
	p.withFields().Debugf("RECV IAC %s", command(c))
	return readAscii, c, false
}

func readCR(_ *telnetProtocol, c byte) (readerState, byte, bool) {
	if c == '\x00' {
		return readAscii, '\r', true
	}
	return readAscii, c, true
}

func readOption(cmd byte) readerState {
	return func(p *telnetProtocol, c byte) (readerState, byte, bool) {
		p.withFields().Debugf("RECV IAC %s %s", command(cmd), command(c))
		opt := p.get(c)
		opt.receive(cmd)
		return readAscii, c, false
	}
}

type commandSender interface {
	sendCommand(...byte) error
}

type optionMap struct {
	cs commandSender
	m  map[byte]*option
}

func newOptionMap(cs commandSender) (o *optionMap) {
	o = &optionMap{cs: cs, m: make(map[byte]*option)}
	return
}

func (o *optionMap) get(c byte) (opt *option) {
	opt, ok := o.m[c]
	if !ok {
		opt = &option{cs: o.cs, code: c}
		o.m[c] = opt
	}
	return
}

func (o *optionMap) merge(m *optionMap) {
	for k, v := range m.m {
		u := o.get(k)
		u.allowUs, u.allowThem = v.allowUs, v.allowThem
		u.us, u.them = v.us, v.them
	}
}

type option struct {
	cs   commandSender
	code byte

	allowUs, allowThem bool
	us, them           telnetQState
}

func (o *option) allow(us, them bool) {
	o.allowUs, o.allowThem = us, them
}

func (o *option) disableThem() {
	o.disable(&o.them, DONT)
}

func (o *option) disableUs() {
	o.disable(&o.us, WONT)
}

func (o *option) disable(state *telnetQState, cmd byte) {
	switch *state {
	case telnetQNo:
		// ignore
	case telnetQYes:
		*state = telnetQWantNoEmpty
		o.cs.sendCommand(cmd, o.code)
	case telnetQWantNoEmpty:
		// ignore
	case telnetQWantNoOpposite:
		*state = telnetQWantNoEmpty
	case telnetQWantYesEmpty:
		*state = telnetQWantYesOpposite
	case telnetQWantYesOpposite:
		// ignore
	}
}

func (o *option) enableThem() {
	o.enable(&o.them, DO)
}

func (o *option) enableUs() {
	o.enable(&o.us, WILL)
}

func (o *option) enable(state *telnetQState, cmd byte) {
	switch *state {
	case telnetQNo:
		*state = telnetQWantYesEmpty
		o.cs.sendCommand(cmd, o.code)
	case telnetQYes:
		// ignore
	case telnetQWantNoEmpty:
		*state = telnetQWantNoOpposite
	case telnetQWantNoOpposite:
		// ignore
	case telnetQWantYesEmpty:
		// ignore
	case telnetQWantYesOpposite:
		*state = telnetQWantYesEmpty
	}
}

func (o *option) receive(req byte) {
	switch req {
	case DO:
		o.receiveEnableRequest(&o.us, o.allowUs, WILL, WONT)
	case DONT:
		o.receiveDisableDemand(&o.us, WILL, WONT)
	case WILL:
		o.receiveEnableRequest(&o.them, o.allowThem, DO, DONT)
	case WONT:
		o.receiveDisableDemand(&o.them, DO, DONT)
	}
}

func (o *option) receiveEnableRequest(state *telnetQState, allowed bool, accept, reject byte) {
	switch *state {
	case telnetQNo:
		if allowed {
			*state = telnetQYes
			o.cs.sendCommand(accept, o.code)
		} else {
			o.cs.sendCommand(reject, o.code)
		}
	case telnetQYes:
		// ignore
	case telnetQWantNoEmpty:
		*state = telnetQNo
	case telnetQWantNoOpposite:
		*state = telnetQYes
	case telnetQWantYesEmpty:
		*state = telnetQYes
	case telnetQWantYesOpposite:
		*state = telnetQWantNoEmpty
		o.cs.sendCommand(reject, o.code)
	}
}

func (o *option) receiveDisableDemand(state *telnetQState, accept, reject byte) {
	switch *state {
	case telnetQNo:
		// ignore
	case telnetQYes:
		*state = telnetQNo
		o.cs.sendCommand(reject, o.code)
	case telnetQWantNoEmpty:
		*state = telnetQNo
	case telnetQWantNoOpposite:
		*state = telnetQWantYesEmpty
		o.cs.sendCommand(accept, o.code)
	case telnetQWantYesEmpty:
		*state = telnetQNo
	case telnetQWantYesOpposite:
		*state = telnetQNo
	}
}

type telnetQState int

const (
	telnetQNo telnetQState = 0 + iota
	telnetQYes
	telnetQWantNoEmpty
	telnetQWantNoOpposite
	telnetQWantYesEmpty
	telnetQWantYesOpposite
)

func (q telnetQState) String() string {
	switch q {
	case telnetQNo:
		return "No"
	case telnetQYes:
		return "Yes"
	case telnetQWantNoEmpty:
		return "WantNo:Empty"
	case telnetQWantNoOpposite:
		return "WantNo:Opposite"
	case telnetQWantYesEmpty:
		return "WantYes:Empty"
	case telnetQWantYesOpposite:
		return "WantYes:Opposite"
	default:
		panic("unknown state")
	}
}
