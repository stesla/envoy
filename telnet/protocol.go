package telnet

import (
	"bytes"
	"io"
	"sync"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

type telnetProtocol struct {
	io.Reader
	io.Writer
	*optionMap
	sync.RWMutex

	peerType PeerType
	in       io.Reader
	out      io.Writer
	state    decodeState
	enc      encoding.Encoding
	log      *maybeLog
}

func newTelnetProtocol(peerType PeerType, r io.Reader, w io.Writer) *telnetProtocol {
	p := &telnetProtocol{
		in:    r,
		out:   w,
		state: decodeByte,
		enc:   ASCII,
		log:   &maybeLog{},
	}
	p.peerType = peerType
	p.optionMap = newOptionMap(p)
	p.Reader = transform.NewReader(p.in, &telnetDecoder{p: p})
	p.Writer = new(bytes.Buffer)
	return p
}

func (p *telnetProtocol) getEncoding() encoding.Encoding {
	p.RLock()
	defer p.RUnlock()
	return p.enc
}

func (p *telnetProtocol) finishCharset(enc encoding.Encoding) {
	if enc != nil {
		p.setEncoding(enc)

		opt := p.get(TransmitBinary)
		if enc == ASCII {
			opt.disableUs()
			opt.disableThem()
		} else {
			opt.enableUs()
			opt.enableThem()
		}
	}
	oldw := p.setWriter(transform.NewWriter(p.out, &telnetEncoder{p: p}))
	if buf, ok := oldw.(*bytes.Buffer); ok {
		buf.WriteTo(p)
	}
}

func (p *telnetProtocol) handleCharset(buf []byte) {
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
		case !opt.enabledForThem() && !opt.enabledForUs():
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

func (p *telnetProtocol) handleSubnegotiation(buf []byte) {
	if len(buf) == 0 {
		p.log.Debug("RECV IAC SB IAC SE")
		return
	}
	switch opt, buf := buf[0], buf[1:]; opt {
	case Charset:
		p.handleCharset(buf)
	default:
		p.log.Debugf("RECV IAC SB %s %q IAC SE", optionByte(opt), buf)
	}
}

func (p *telnetProtocol) notify(o *option) {
	switch o.code {
	case Charset:
		enabled := o.enabledForUs() || o.enabledForThem()
		if p.peerType == ClientType && enabled {
			p.startCharset()
		} else if !enabled && !(o.negotiatingThem() || o.negotiatingUs()) {
			p.finishCharset(nil)
		}
	}
}

func (p *telnetProtocol) send(cmd ...byte) (err error) {
	_, err = p.out.Write(cmd)
	return
}

func (p *telnetProtocol) selectEncoding(names [][]byte) (charset []byte, enc encoding.Encoding) {
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

func (p *telnetProtocol) sendCharsetRejected() {
	p.log.Debug("SENT IAC SB CHARSET REJECTED IAC SE")
	p.send(IAC, SB, Charset, charsetRejected, IAC, SE)
}

func (p *telnetProtocol) setWriter(new io.Writer) (old io.Writer) {
	p.Lock()
	defer p.Unlock()
	old = p.Writer
	p.Writer = new
	return
}

func (p *telnetProtocol) setEncoding(enc encoding.Encoding) {
	p.log.Tracef("setEncoding(%q)", enc)
	p.Lock()
	defer p.Unlock()
	p.enc = enc
}

func (p *telnetProtocol) SetLog(log Log) {
	p.log.log = log
}

func (p *telnetProtocol) startCharset() {
	p.Lock()
	defer p.Unlock()
	p.log.Debug("SENT IAC SB CHARSET REQUEST \";UTF-8;US-ASCII\" IAC SE")
	out := []byte{IAC, SB, Charset, charsetRequest}
	out = append(out, []byte(";UTF-8;US-ASCII")...)
	out = append(out, IAC, SE)
	p.send(out...)
}

type telnetDecoder struct {
	p *telnetProtocol
}

func (*telnetDecoder) Reset() {}

func (d *telnetDecoder) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	buf := make([]byte, len(dst))
	n := 0
	telnet := d.p
	enc := telnet.getEncoding()
	for i, b := range src {
		if n >= len(buf) {
			err = transform.ErrShortDst
			break
		}

		var c byte
		var ok bool
		telnet.state, c, ok = telnet.state(telnet, b)
		if ok {
			buf[n] = c
			n++
		}

		nSrc = i + 1

		if newEnc := telnet.getEncoding(); enc != newEnc {
			eof := atEOF && len(buf) == len(src)
			nDst, _, err = enc.NewDecoder().Transform(dst, buf[:n], eof)
			if err != nil {
				return
			}
			err = transform.ErrShortDst
			return
		}
	}
	nDst, _, terr := telnet.getEncoding().NewDecoder().Transform(dst, buf[:n], atEOF)
	if nil == err {
		err = terr
	}
	return
}

type decodeState func(*telnetProtocol, byte) (decodeState, byte, bool)

func decodeByte(_ *telnetProtocol, c byte) (decodeState, byte, bool) {
	switch c {
	case IAC:
		return decodeCommand, c, false
	case '\r':
		return decodeCR, c, false
	}
	return decodeByte, c, true
}

func decodeCommand(p *telnetProtocol, c byte) (decodeState, byte, bool) {
	switch c {
	case IAC:
		p.log.Trace("RECV IAC IAC")
		return decodeByte, c, true
	case DO, DONT, WILL, WONT:
		return decodeOption(c), c, false
	case SB:
		return decodeSubnegotiation, c, false
	case NOP:
		p.log.Trace("RECV IAC NOP")
	default:
		p.log.Debugf("RECV IAC %s", commandByte(c))
	}
	return decodeByte, c, false
}

func decodeCR(_ *telnetProtocol, c byte) (decodeState, byte, bool) {
	if c == '\x00' {
		return decodeByte, '\r', true
	}
	return decodeByte, c, true
}

func decodeOption(cmd byte) decodeState {
	return func(p *telnetProtocol, c byte) (decodeState, byte, bool) {
		opt := p.get(c)
		opt.receive(cmd)
		return decodeByte, c, false
	}
}

const subnegotiationBufferSize = 256

func decodeSubnegotiation(_ *telnetProtocol, option byte) (decodeState, byte, bool) {
	var buf = make([]byte, 0, subnegotiationBufferSize)
	buf = append(buf, option)

	var readByte, seenIAC decodeState

	readByte = func(p *telnetProtocol, c byte) (decodeState, byte, bool) {
		switch c {
		case IAC:
			return seenIAC, c, false
		default:
			buf = append(buf, c)
		}
		return readByte, c, false
	}

	seenIAC = func(p *telnetProtocol, c byte) (decodeState, byte, bool) {
		switch c {
		case IAC:
			return readByte, IAC, true
		case SE:
			p.handleSubnegotiation(buf)
		}
		return decodeByte, c, false
	}

	return readByte, option, false
}

type telnetEncoder struct {
	p *telnetProtocol
}

func (*telnetEncoder) Reset() {}

func (d *telnetEncoder) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	srcbuf := make([]byte, len(src))
	n, _, terr := d.p.getEncoding().NewEncoder().Transform(srcbuf, src, atEOF)
	for i, b := range srcbuf[:n] {
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
	if nil == err {
		err = terr
	}
	return
}
