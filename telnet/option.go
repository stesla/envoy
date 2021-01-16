package telnet

type Option interface {
	Allow(us, them bool)
	DisableThem()
	DisableUs()
	EnabledForUs() bool
	EnabledForThem() bool
	EnableThem()
	EnableUs()
	NegotiatingThem() bool
	NegotiatingUs() bool
}

type OptionHandler interface {
	Code() byte
	HandleSubnegotiation([]byte)
	HandleOption(Option)
	Register(Protocol)
}

type protocol interface {
	Send(...byte) error
	notify(*option)
}

type optionMap struct {
	p protocol
	m map[byte]*option
}

func newOptionMap(p protocol) (o *optionMap) {
	o = &optionMap{p: p, m: make(map[byte]*option)}
	return
}

func (o *optionMap) get(c byte) (opt *option) {
	opt, ok := o.m[c]
	if !ok {
		opt = newOption(c, o.p)
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

// RFC 1143 - The Q Method of Implementing TELNET Option Negotiation

type option struct {
	p    protocol
	code byte

	allowUs, allowThem bool
	us, them           telnetQState

	themchs map[chan option]struct{}
	uschs   map[chan option]struct{}
}

func newOption(c byte, p protocol) *option {
	return &option{
		p:       p,
		code:    c,
		themchs: make(map[chan option]struct{}),
		uschs:   make(map[chan option]struct{}),
	}
}

func (o *option) Allow(us, them bool) {
	o.allowUs, o.allowThem = us, them
}

func (o *option) DisableThem() {
	o.disable(&o.them, DONT)
}

func (o *option) DisableUs() {
	o.disable(&o.us, WONT)
}

func (o *option) disable(state *telnetQState, cmd byte) {
	switch *state {
	case telnetQNo:
		// ignore
	case telnetQYes:
		*state = telnetQWantNoEmpty
		o.Send(cmd, o.code)
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

func (o *option) EnabledForThem() bool {
	return telnetQYes == o.them
}

func (o *option) EnabledForUs() bool {
	return telnetQYes == o.us
}

func (o *option) EnableThem() {
	o.enable(&o.them, DO)
}

func (o *option) EnableUs() {
	o.enable(&o.us, WILL)
}

func (o *option) enable(state *telnetQState, cmd byte) {
	switch *state {
	case telnetQNo:
		*state = telnetQWantYesEmpty
		o.Send(cmd, o.code)
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

func (o *option) NegotiatingThem() bool {
	return telnetQNo != o.them && telnetQYes != o.them
}

func (o *option) NegotiatingUs() bool {
	return telnetQNo != o.us && telnetQYes != o.us
}

func (o *option) notify() {
	o.p.notify(o)
}

func (o *option) receive(req byte) {
	if p, ok := o.p.(*telnetProtocol); ok {
		p.log.Debugf("RECV IAC %s %s", commandByte(req), optionByte(o.code))
	}
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
			o.Send(accept, o.code)
		} else {
			o.Send(reject, o.code)
		}
	case telnetQYes:
		// ignore
	case telnetQWantNoEmpty:
		*state = telnetQNo
		o.notify()
	case telnetQWantNoOpposite:
		*state = telnetQYes
		o.notify()
	case telnetQWantYesEmpty:
		*state = telnetQYes
		o.notify()
	case telnetQWantYesOpposite:
		*state = telnetQWantNoEmpty
		o.Send(reject, o.code)
	}
}

func (o *option) receiveDisableDemand(state *telnetQState, accept, reject byte) {
	switch *state {
	case telnetQNo:
		// ignore
	case telnetQYes:
		*state = telnetQNo
		o.Send(reject, o.code)
		o.notify()
	case telnetQWantNoEmpty:
		*state = telnetQNo
		o.notify()
	case telnetQWantNoOpposite:
		*state = telnetQWantYesEmpty
		o.Send(accept, o.code)
	case telnetQWantYesEmpty:
		*state = telnetQNo
		o.notify()
	case telnetQWantYesOpposite:
		*state = telnetQNo
		o.notify()
	}
}

func (o *option) Send(cmd, option byte) {
	if p, ok := o.p.(*telnetProtocol); ok {
		p.log.Debugf("SENT IAC %s %s", commandByte(cmd), optionByte(option))
	}
	o.p.Send(IAC, cmd, option)
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

func (telnetQ telnetQState) String() string {
	switch telnetQ {
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
