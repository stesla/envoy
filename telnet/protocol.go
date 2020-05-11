package telnet

import (
	"io"

	log "github.com/sirupsen/logrus"
	"golang.org/x/text/encoding"
	"golang.org/x/text/transform"
)

type telnetProtocol struct {
	ctype  ConnType
	fields log.Fields
	in     io.Reader
	out    io.Writer
	state  readerState

	io.Reader
	io.Writer

	*optionMap
}

func newTelnetProtocol(fields log.Fields, r io.Reader, w io.Writer) *telnetProtocol {
	p := &telnetProtocol{
		fields: fields,
		in:     r,
		out:    w,
		state:  readAscii,
	}
	p.ctype = fields["type"].(ConnType)
	p.optionMap = newOptionMap(p)
	p.setEncoding(ASCII)
	return p
}

func (p *telnetProtocol) withFields() *log.Entry {
	return log.WithFields(p.fields)
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

func (p *telnetProtocol) setEncoding(enc encoding.Encoding) {
	p.Reader = transform.NewReader(p.in, transform.Chain(&telnetDecoder{p: p}, enc.NewDecoder()))
	p.Writer = transform.NewWriter(p.out, transform.Chain(&telnetEncoder{p: p}, enc.NewEncoder()))
}

type telnetDecoder struct {
	p *telnetProtocol
}

func (*telnetDecoder) Reset() {}

func (d *telnetDecoder) Transform(dst, src []byte, _ bool) (nDst, nSrc int, err error) {
	telnet := d.p
	for i, b := range src {
		if nDst >= len(dst) {
			err = transform.ErrShortDst
			break
		}

		var c byte
		var ok bool
		telnet.state, c, ok = telnet.state(telnet, b)
		if ok {
			dst[nDst] = c
			nDst++
		}

		nSrc = i + 1
	}
	return
}

type telnetEncoder struct {
	p *telnetProtocol
}

func (*telnetEncoder) Reset() {}

func (d *telnetEncoder) Transform(dst, src []byte, _ bool) (nDst, nSrc int, err error) {
	for i, b := range src {
		var buf []byte
		switch b {
		case IAC:
			buf = []byte{IAC, IAC}
		case '\n':
			buf = []byte("\r\n")
		case '\r':
			buf = []byte("\r\x00")
		default:
			buf = []byte{b}
		}
		if nDst+len(buf) < len(dst) {
			nDst += copy(dst[nDst:], buf)
		} else {
			err = transform.ErrShortDst
			break
		}
		nSrc = i + 1
	}
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
