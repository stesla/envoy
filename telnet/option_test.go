package telnet

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDONotSupportEcho(t *testing.T) {
	tests := []struct {
		command, response      byte
		message                string
		usEnabled, themEnabled bool
	}{
		{WILL, DONT, "WILL", false, false},
		{DO, WONT, "DO", false, false},
		{WONT, DONT, "WONT", false, true},
		// This case will never happen, since we don't support the
		// option and would never enable it, but to thoroughly test the
		// option negotiation, I am adding it for completeness. What
		// will _actually_ happen if we receive IAC DONT ECHO is
		// absolutely nothing, because we already have it disabled, so
		// we'll ignore it per the Q Method (RFC 1143).
		{DONT, WONT, "DONT", true, false},
	}
	for _, test := range tests {
		t.Logf("testOption %s", test.message)

		dt := newDecodeTest([]byte{'h', IAC, test.command, Echo, 'i'})
		o := dt.p.get(Echo)
		if test.usEnabled {
			o.us = telnetQYes
		}
		if test.themEnabled {
			o.them = telnetQYes
		}
		r, w, err := dt.decode()
		assert.NoError(t, err)
		assert.Equal(t, []byte("hi"), r)
		assert.Equal(t, []byte{IAC, test.response, Echo}, w)
	}
}

type qMethodTest struct {
	start, end telnetQState
	permitted  bool
	expected   byte
	actual     []byte
}

func (q *qMethodTest) send(actual ...byte) error {
	q.actual = actual
	return nil
}

func (q *qMethodTest) shouldNotify() bool {
	return (q.start != telnetQNo && q.start != telnetQYes) &&
		(q.end == telnetQNo || q.end == telnetQYes)
}

func (q *qMethodTest) assertNotification(t *testing.T, ch <-chan option, f func(option) telnetQState) {
	if q.shouldNotify() {
		select {
		case opt := <-ch:
			if !assert.Equal(t, f(opt), q.end) {
				t.Logf("%+v", opt)
			}
		default:
			assert.Fail(t, "expected notification, but none was sent")
		}
	}
}

func TestQMethodReceiveDO(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, permitted: false, end: telnetQNo, expected: WONT},
		&qMethodTest{start: telnetQNo, permitted: true, end: telnetQYes, expected: WILL},
		&qMethodTest{start: telnetQYes, end: telnetQYes},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQYes},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQYes},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantNoEmpty, expected: WONT},
	}
	for _, q := range tests {
		o := newOption(SuppressGoAhead, q)
		o.us, o.allowUs = q.start, q.permitted
		ch := o.notifyOfUs()
		o.receive(DO)
		assert.Equalf(t, q.end, o.us, "expected %s got %s", q.end, o.us)
		if q.expected != 0 {
			assert.Equal(t, []byte{IAC, q.expected, SuppressGoAhead}, q.actual)
		}
		q.assertNotification(t, ch, func(opt option) telnetQState { return opt.us })
	}
}

func TestQMethodReceiveDONT(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQNo},
		&qMethodTest{start: telnetQYes, end: telnetQNo, expected: WONT},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantYesEmpty, expected: WILL},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQNo},
	}
	for _, q := range tests {
		o := newOption(SuppressGoAhead, q)
		o.us, o.allowUs = q.start, q.permitted
		ch := o.notifyOfUs()
		o.receive(DONT)
		assert.Equalf(t, q.end, o.us, "expected %s got %s", q.end, o.us)
		if q.expected != 0 {
			assert.Equal(t, []byte{IAC, q.expected, SuppressGoAhead}, q.actual)
		}
		q.assertNotification(t, ch, func(opt option) telnetQState { return opt.us })
	}
}

func TestQMethodReceiveWILL(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, permitted: false, end: telnetQNo, expected: DONT},
		&qMethodTest{start: telnetQNo, permitted: true, end: telnetQYes, expected: DO},
		&qMethodTest{start: telnetQYes, end: telnetQYes},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQYes},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQYes},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantNoEmpty, expected: DONT},
	}
	for _, q := range tests {
		o := newOption(SuppressGoAhead, q)
		o.them, o.allowThem = q.start, q.permitted
		ch := o.notifyOfThem()
		o.receive(WILL)
		assert.Equalf(t, q.end, o.them, "expected %s got %s", q.end, o.them)
		if q.expected != 0 {
			assert.Equal(t, []byte{IAC, q.expected, SuppressGoAhead}, q.actual)
		}
		q.assertNotification(t, ch, func(opt option) telnetQState { return opt.them })
	}
}

func TestQMethodReceiveWONT(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQNo},
		&qMethodTest{start: telnetQYes, end: telnetQNo, expected: DONT},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantYesEmpty, expected: DO},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQNo},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQNo},
	}
	for _, q := range tests {
		o := newOption(SuppressGoAhead, q)
		o.them, o.allowThem = q.start, q.permitted
		ch := o.notifyOfThem()
		o.receive(WONT)
		assert.Equalf(t, q.end, o.them, "expected %s got %s", q.end, o.them)
		if q.expected != 0 {
			assert.Equal(t, []byte{IAC, q.expected, SuppressGoAhead}, q.actual)
		}
		q.assertNotification(t, ch, func(opt option) telnetQState { return opt.them })
	}
}

func TestQMethodAskEnableThem(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQWantYesEmpty, expected: DO},
		&qMethodTest{start: telnetQYes, end: telnetQYes},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQWantNoOpposite},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantNoOpposite},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQWantYesEmpty},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantYesEmpty},
	}
	for _, q := range tests {
		o := newOption(SuppressGoAhead, q)
		o.them = q.start
		o.enableThem()
		assert.Equalf(t, q.end, o.them, "expected %s got %s", q.end, o.them)
		if q.expected != 0 {
			assert.Equal(t, []byte{IAC, q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodDisableThem(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQNo},
		&qMethodTest{start: telnetQYes, end: telnetQWantNoEmpty, expected: DONT},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQWantNoEmpty},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantNoEmpty},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQWantYesOpposite},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantYesOpposite},
	}
	for _, q := range tests {
		o := newOption(SuppressGoAhead, q)
		o.them = q.start
		o.disableThem()
		assert.Equalf(t, q.end, o.them, "expected %s got %s", q.end, o.them)
		if q.expected != 0 {
			assert.Equal(t, []byte{IAC, q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodEnableUs(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQWantYesEmpty, expected: WILL},
		&qMethodTest{start: telnetQYes, end: telnetQYes},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQWantNoOpposite},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantNoOpposite},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQWantYesEmpty},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantYesEmpty},
	}
	for _, q := range tests {
		o := newOption(SuppressGoAhead, q)
		o.us = q.start
		o.enableUs()
		assert.Equalf(t, q.end, o.us, "expected %s got %s", q.end, o.us)
		if q.expected != 0 {
			assert.Equal(t, []byte{IAC, q.expected, SuppressGoAhead}, q.actual)
		}
	}
}

func TestQMethodDisableUs(t *testing.T) {
	tests := []*qMethodTest{
		&qMethodTest{start: telnetQNo, end: telnetQNo},
		&qMethodTest{start: telnetQYes, end: telnetQWantNoEmpty, expected: WONT},
		&qMethodTest{start: telnetQWantNoEmpty, end: telnetQWantNoEmpty},
		&qMethodTest{start: telnetQWantNoOpposite, end: telnetQWantNoEmpty},
		&qMethodTest{start: telnetQWantYesEmpty, end: telnetQWantYesOpposite},
		&qMethodTest{start: telnetQWantYesOpposite, end: telnetQWantYesOpposite},
	}
	for _, q := range tests {
		o := newOption(SuppressGoAhead, q)
		o.us = q.start
		o.disableUs()
		assert.Equalf(t, q.end, o.us, "expected %s got %s", q.end, o.us)
		if q.expected != 0 {
			assert.Equal(t, []byte{IAC, q.expected, SuppressGoAhead}, q.actual)
		}
	}
}
