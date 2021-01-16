package telnet

import (
	"bytes"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/unicode"
)

type CharsetOption struct {
	p Protocol
}

func (c *CharsetOption) Code() byte { return Charset }

func (c *CharsetOption) finishCharset(enc encoding.Encoding) {
	if enc != nil {
		c.p.SetEncoding(enc)

		opt := c.p.GetOption(TransmitBinary)
		if enc == ASCII {
			opt.DisableUs()
			opt.DisableThem()
		} else {
			opt.EnableUs()
			opt.EnableThem()
		}
	}

	p := c.p.(*telnetProtocol)
	p.flushBuffer()
}

func (c *CharsetOption) HandleSubnegotiation(buf []byte) {
	if len(buf) == 0 {
		c.p.Log().Debug("RECV IAC SB CHARSET IAC SE")
		return
	}
	cmd, buf := buf[0], buf[1:]
	c.p.Log().Debugf("RECV IAC SB CHARSET %s %q IAC SE", charsetByte(cmd), buf)
	opt := c.p.GetOption(Charset)
	switch cmd {
	case charsetRequest:
		switch {
		case c.p.PeerType() == ClientType:
			if string(buf) == "UTF-8" {
				c.finishCharset(unicode.UTF8)
				return
			}
			fallthrough
		case !opt.EnabledForThem() && !opt.EnabledForUs():
			c.sendCharsetRejected()
			return
		}

		const ttable = "[TTABLE]"
		if len(buf) > 10 && bytes.HasPrefix(buf, []byte(ttable)) {
			// strip off the version byte
			buf = buf[len(ttable)+1:]
		}
		if len(buf) < 2 {
			c.sendCharsetRejected()
			return
		}

		charset, encoding := c.selectEncoding(bytes.Split(buf[1:], buf[0:1]))
		if encoding == nil {
			c.sendCharsetRejected()
			return
		}

		c.p.Log().Debugf("SEND IAC SB CHARSET ACCEPTED %q IAC SE", charset)
		cmd := []byte{IAC, SB, Charset, charsetAccepted}
		cmd = append(cmd, []byte(charset)...)
		cmd = append(cmd, IAC, SE)
		c.p.Send(cmd...)
		c.finishCharset(encoding)

	case charsetAccepted:
		_, encoding := c.selectEncoding([][]byte{buf})
		if encoding != nil {
			c.finishCharset(encoding)
		}

	case charsetRejected:
		c.finishCharset(nil)

	case charsetTTableIs:
	case charsetTTableRejected:
	case charsetTTableAck:
	case charsetTTableNak:
	}
}

func (c *CharsetOption) HandleOption(o Option) {
	enabled := o.EnabledForUs() || o.EnabledForThem()
	if c.p.PeerType() == ClientType && enabled {
		c.startCharset()
	} else if !enabled && !(o.NegotiatingThem() || o.NegotiatingUs()) {
		c.finishCharset(nil)
	}
}

func (c *CharsetOption) Register(p Protocol) { c.p = p }

func (c *CharsetOption) selectEncoding(names [][]byte) (charset []byte, enc encoding.Encoding) {
	for _, name := range names {
		switch string(name) {
		case "UTF-8":
			return name, unicode.UTF8
		case "US-ASCII":
			charset, enc = name, ASCII
		}
	}
	return
}

func (c *CharsetOption) sendCharsetRejected() {
	c.p.Log().Debug("SENT IAC SB CHARSET REJECTED IAC SE")
	c.p.Send(IAC, SB, Charset, charsetRejected, IAC, SE)
}

func (c *CharsetOption) startCharset() {
	c.p.Lock()
	defer c.p.Unlock()
	c.p.Log().Debug("SENT IAC SB CHARSET REQUEST \";UTF-8;US-ASCII\" IAC SE")
	out := []byte{IAC, SB, Charset, charsetRequest}
	out = append(out, []byte(";UTF-8;US-ASCII")...)
	out = append(out, IAC, SE)
	c.p.Send(out...)
}
