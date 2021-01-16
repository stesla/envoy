package telnet

import (
	"bytes"

	"golang.org/x/text/encoding/unicode"
)

type CharsetOption struct {
	p Protocol
}

func (c *CharsetOption) Code() byte { return Charset }

func (c *CharsetOption) HandleSubnegotiation(buf []byte) {
	p := c.p.(*telnetProtocol)

	if len(buf) == 0 {
		p.log.Debug("RECV IAC SB CHARSET IAC SE")
		return
	}
	cmd, buf := buf[0], buf[1:]
	p.log.Debugf("RECV IAC SB CHARSET %s %q IAC SE", charsetByte(cmd), buf)
	opt := p.get(Charset)
	switch cmd {
	case charsetRequest:
		switch {
		case p.peerType == ClientType:
			if string(buf) == "UTF-8" {
				p.finishCharset(unicode.UTF8)
				return
			}
			fallthrough
		case !opt.EnabledForThem() && !opt.EnabledForUs():
			p.sendCharsetRejected()
			return
		}

		const ttable = "[TTABLE]"
		if len(buf) > 10 && bytes.HasPrefix(buf, []byte(ttable)) {
			// strip off the version byte
			buf = buf[len(ttable)+1:]
		}
		if len(buf) < 2 {
			p.sendCharsetRejected()
			return
		}

		charset, encoding := p.selectEncoding(bytes.Split(buf[1:], buf[0:1]))
		if encoding == nil {
			p.sendCharsetRejected()
			return
		}

		p.log.Debugf("SEND IAC SB CHARSET ACCEPTED %q IAC SE", charset)
		cmd := []byte{IAC, SB, Charset, charsetAccepted}
		cmd = append(cmd, []byte(charset)...)
		cmd = append(cmd, IAC, SE)
		p.send(cmd...)
		p.finishCharset(encoding)

	case charsetAccepted:
		_, encoding := p.selectEncoding([][]byte{buf})
		if encoding != nil {
			p.finishCharset(encoding)
		}

	case charsetRejected:
		p.finishCharset(nil)

	case charsetTTableIs:
	case charsetTTableRejected:
	case charsetTTableAck:
	case charsetTTableNak:
	}
}

func (c *CharsetOption) HandleOption(o Option) {
	p := c.p.(*telnetProtocol)

	enabled := o.EnabledForUs() || o.EnabledForThem()
	if p.peerType == ClientType && enabled {
		p.startCharset()
	} else if !enabled && !(o.NegotiatingThem() || o.NegotiatingUs()) {
		p.finishCharset(nil)
	}
}

func (c *CharsetOption) Register(p Protocol) { c.p = p }
