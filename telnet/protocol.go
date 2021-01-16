package telnet

import (
	"io"
	"sync"

	"golang.org/x/text/encoding"
	"golang.org/x/text/transform"
)

type Protocol interface {
	GetOption(byte) Option
	Log() Log
	PeerType() PeerType
	Send(...byte) error
	SetEncoding(encoding.Encoding)
	SetWriter(io.Writer) io.Writer

	sync.Locker
	io.Writer
}

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

	handlers map[byte]OptionHandler
}

func newTelnetProtocol(peerType PeerType, r io.Reader, w io.Writer) *telnetProtocol {
	p := &telnetProtocol{
		in:       r,
		out:      w,
		state:    decodeByte,
		enc:      ASCII,
		log:      &maybeLog{},
		handlers: map[byte]OptionHandler{},
	}
	p.peerType = peerType
	p.optionMap = newOptionMap(p)
	p.Reader = transform.NewReader(p.in, &telnetDecoder{p: p})
	p.Writer = transform.NewWriter(p.out, &telnetEncoder{p: p})
	return p
}

func (p *telnetProtocol) getEncoding() encoding.Encoding {
	p.RLock()
	defer p.RUnlock()
	return p.enc
}

func (p *telnetProtocol) GetOption(code byte) Option {
	return p.get(code)
}

func (p *telnetProtocol) handleSubnegotiation(buf []byte) {
	if len(buf) == 0 {
		p.log.Debug("RECV IAC SB IAC SE")
		return
	}
	opt, buf := buf[0], buf[1:]
	if h, ok := p.handlers[opt]; ok {
		h.HandleSubnegotiation(buf)
	} else {
		p.log.Debugf("RECV IAC SB %s %q IAC SE", optionByte(opt), buf)
	}
}

func (p *telnetProtocol) Log() Log { return p.log }

func (p *telnetProtocol) notify(o *option) {
	if h, ok := p.handlers[o.code]; ok {
		h.HandleOption(o)
	}
}

func (p *telnetProtocol) PeerType() PeerType { return p.peerType }

func (p *telnetProtocol) RegisterHandler(h OptionHandler) {
	h.Register(p)
	p.handlers[h.Code()] = h
}

func (p *telnetProtocol) Send(cmd ...byte) (err error) {
	_, err = p.out.Write(cmd)
	return
}

func (p *telnetProtocol) SetWriter(new io.Writer) (old io.Writer) {
	p.Lock()
	defer p.Unlock()
	old = p.Writer
	p.Writer = new
	return
}

func (p *telnetProtocol) SetEncoding(enc encoding.Encoding) {
	p.log.Tracef("SetEncoding(%q)", enc)
	p.Lock()
	defer p.Unlock()
	p.enc = enc
}

func (p *telnetProtocol) SetLog(log Log) {
	p.log.log = log
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
