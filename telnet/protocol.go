package telnet

import (
	"io"
)

type telnetProtocol struct {
	in    io.Reader
	out   io.Writer
	state readerState

	options map[byte]*option
}

func newTelnetProtocol(r io.Reader, w io.Writer) *telnetProtocol {
	return &telnetProtocol{
		in:      r,
		out:     w,
		state:   readAscii,
		options: make(map[byte]*option),
	}
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
		case InterpretAsCommand:
			err = p.sendCommand(InterpretAsCommand)
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

func (p *telnetProtocol) getOption(c byte) (o *option) {
	o, ok := p.options[c]
	if !ok {
		o = &option{code: c}
		p.options[c] = o
	}
	return
}

func (p *telnetProtocol) sendCommand(cmd ...byte) (err error) {
	cmd = append([]byte{InterpretAsCommand}, cmd...)
	_, err = p.out.Write(cmd)
	return
}

type readerState func(*telnetProtocol, byte) (readerState, byte, bool)

func readAscii(_ *telnetProtocol, c byte) (readerState, byte, bool) {
	switch c {
	case InterpretAsCommand:
		return readCommand, c, false
	case '\r':
		return readCR, c, false
	}
	return readAscii, c, true
}

func readCommand(_ *telnetProtocol, c byte) (readerState, byte, bool) {
	switch c {
	case InterpretAsCommand:
		return readAscii, c, true
	case Do, Dont, Will, Wont:
		return readOption(c), c, false
	}
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
		opt := p.getOption(c)
		opt.receive(p, cmd)
		return readAscii, c, false
	}
}

type commandSender interface {
	sendCommand(...byte) error
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

type option struct {
	code byte

	allowUs, allowThem bool
	us, them           telnetQState
}

func (o *option) disableThem(cs commandSender) {
	o.disable(cs, &o.them, Dont)
}

func (o *option) disableUs(cs commandSender) {
	o.disable(cs, &o.us, Wont)
}

func (o *option) disable(cs commandSender, state *telnetQState, cmd byte) {
	switch *state {
	case telnetQNo:
		// ignore
	case telnetQYes:
		*state = telnetQWantNoEmpty
		cs.sendCommand(cmd, o.code)
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

func (o *option) enableThem(cs commandSender) {
	o.enable(cs, &o.them, Do)
}

func (o *option) enableUs(cs commandSender) {
	o.enable(cs, &o.us, Will)
}

func (o *option) enable(cs commandSender, state *telnetQState, cmd byte) {
	switch *state {
	case telnetQNo:
		*state = telnetQWantYesEmpty
		cs.sendCommand(cmd, o.code)
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

func (o *option) receive(cs commandSender, req byte) {
	switch req {
	case Do:
		o.receiveEnableRequest(cs, &o.us, o.allowUs, Will, Wont)
	case Dont:
		o.receiveDisableDemand(cs, &o.us, Will, Wont)
	case Will:
		o.receiveEnableRequest(cs, &o.them, o.allowThem, Do, Dont)
	case Wont:
		o.receiveDisableDemand(cs, &o.them, Do, Dont)
	}
}

func (o *option) receiveEnableRequest(cs commandSender, state *telnetQState, allowed bool, accept, reject byte) {
	switch *state {
	case telnetQNo:
		if allowed {
			*state = telnetQYes
			cs.sendCommand(accept, o.code)
		} else {
			cs.sendCommand(reject, o.code)
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
		cs.sendCommand(reject, o.code)
	}
}

func (o *option) receiveDisableDemand(cs commandSender, state *telnetQState, accept, reject byte) {
	switch *state {
	case telnetQNo:
		// ignore
	case telnetQYes:
		*state = telnetQNo
		cs.sendCommand(reject, o.code)
	case telnetQWantNoEmpty:
		*state = telnetQNo
	case telnetQWantNoOpposite:
		*state = telnetQWantYesEmpty
		cs.sendCommand(accept, o.code)
	case telnetQWantYesEmpty:
		*state = telnetQNo
	case telnetQWantYesOpposite:
		*state = telnetQNo
	}
}
