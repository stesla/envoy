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
	state  decodeState

	io.Reader
	io.Writer

	*optionMap
}

func newTelnetProtocol(fields log.Fields, r io.Reader, w io.Writer) *telnetProtocol {
	p := &telnetProtocol{
		fields: fields,
		in:     r,
		out:    w,
		state:  decodeByte,
	}
	p.ctype = fields["type"].(ConnType)
	p.optionMap = newOptionMap(p)
	p.setEncoding(ASCII)
	return p
}

func (p *telnetProtocol) withFields() *log.Entry {
	return log.WithFields(p.fields)
}

func (p *telnetProtocol) send(cmd ...byte) (err error) {
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
		p.withFields().Debug("RECV IAC IAC")
		return decodeByte, c, true
	case DO, DONT, WILL, WONT:
		return decodeOption(c), c, false
	}
	p.withFields().Debugf("RECV IAC %s", commandByte(c))
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
