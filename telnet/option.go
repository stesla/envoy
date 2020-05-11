package telnet

type sender interface {
	send(...byte) error
}

type optionMap struct {
	s sender
	m map[byte]*option
}

func newOptionMap(s sender) (o *optionMap) {
	o = &optionMap{s: s, m: make(map[byte]*option)}
	return
}

func (o *optionMap) get(c byte) (opt *option) {
	opt, ok := o.m[c]
	if !ok {
		opt = newOption(c, o.s)
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
	s    sender
	code byte

	allowUs, allowThem bool
	us, them           telnetQState

	themchs map[chan option]struct{}
	uschs   map[chan option]struct{}
}

func newOption(c byte, s sender) *option {
	return &option{
		s:       s,
		code:    c,
		themchs: make(map[chan option]struct{}),
		uschs:   make(map[chan option]struct{}),
	}
}

func (o *option) allow(us, them bool) {
	o.allowUs, o.allowThem = us, them
}

func (o *option) disableThem() <-chan option {
	o.disable(&o.them, DONT)
	return o.notifyOfThem()
}

func (o *option) disableUs() <-chan option {
	o.disable(&o.us, WONT)
	return o.notifyOfUs()
}

func (o *option) disable(state *telnetQState, cmd byte) {
	switch *state {
	case telnetQNo:
		// ignore
	case telnetQYes:
		*state = telnetQWantNoEmpty
		o.send(cmd, o.code)
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

func (o *option) enabledForThem() bool {
	return telnetQYes == o.them
}

func (o *option) enabledForUs() bool {
	return telnetQYes == o.us
}

func (o *option) enableThem() <-chan option {
	o.enable(&o.them, DO)
	return o.notifyOfThem()
}

func (o *option) enableUs() <-chan option {
	o.enable(&o.us, WILL)
	return o.notifyOfUs()
}

func (o *option) enable(state *telnetQState, cmd byte) {
	switch *state {
	case telnetQNo:
		*state = telnetQWantYesEmpty
		o.send(cmd, o.code)
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

func (o *option) notifyOfThem() <-chan option {
	ch := make(chan option, 1)
	o.themchs[ch] = struct{}{}
	return ch
}

func (o *option) notifyOfUs() <-chan option {
	ch := make(chan option, 1)
	o.uschs[ch] = struct{}{}
	return ch
}

func (o *option) notify(state *telnetQState) {
	var m map[chan option]struct{}
	if state == &o.them {
		m = o.themchs
	} else if state == &o.us {
		m = o.uschs
	} else {
		panic("neither us or them")
	}

	for ch, _ := range m {
		delete(m, ch)
		ch <- *o
	}
}

func (o *option) receive(req byte) {
	if p, ok := o.s.(*telnetProtocol); ok {
		p.withFields().Debugf("RECV IAC %s %s", commandByte(req), optionByte(o.code))
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
			o.send(accept, o.code)
		} else {
			o.send(reject, o.code)
		}
	case telnetQYes:
		// ignore
	case telnetQWantNoEmpty:
		*state = telnetQNo
		o.notify(state)
	case telnetQWantNoOpposite:
		*state = telnetQYes
		o.notify(state)
	case telnetQWantYesEmpty:
		*state = telnetQYes
		o.notify(state)
	case telnetQWantYesOpposite:
		*state = telnetQWantNoEmpty
		o.send(reject, o.code)
	}
}

func (o *option) receiveDisableDemand(state *telnetQState, accept, reject byte) {
	switch *state {
	case telnetQNo:
		// ignore
	case telnetQYes:
		*state = telnetQNo
		o.send(reject, o.code)
		o.notify(state)
	case telnetQWantNoEmpty:
		*state = telnetQNo
		o.notify(state)
	case telnetQWantNoOpposite:
		*state = telnetQWantYesEmpty
		o.send(accept, o.code)
	case telnetQWantYesEmpty:
		*state = telnetQNo
		o.notify(state)
	case telnetQWantYesOpposite:
		*state = telnetQNo
		o.notify(state)
	}
}

func (o *option) send(cmd, option byte) {
	if p, ok := o.s.(*telnetProtocol); ok {
		p.withFields().Debugf("SENT IAC %s %s", commandByte(cmd), optionByte(option))
	}
	o.s.send(IAC, cmd, option)
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
