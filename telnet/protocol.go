package telnet

import (
	"io"
)

type telnetProtocol struct {
	in    io.Reader
	out   io.Writer
	state readerState
}

func newTelnetProtocol(in io.Reader, out io.Writer) *telnetProtocol {
	return &telnetProtocol{in, out, readAscii}
}

func (p *telnetProtocol) Read(b []byte) (n int, err error) {
	buf := make([]byte, len(b))
	nr, err := p.in.Read(buf)
	buf = buf[:nr]
	for len(buf) > 0 && n < len(b) {
		var ok bool
		p.state, ok = p.state(p, buf[0])
		if ok {
			b[n] = buf[0]
			n++
		}
		buf = buf[1:]
	}
	return n, err
}

func (p *telnetProtocol) Write(b []byte) (n int, err error) {
	for n = 0; len(b) > 0 && err == nil; n++ {
		if b[0] == InterpretAsCommand {
			err = p.sendCommand(InterpretAsCommand)
		} else {
			_, err = p.out.Write(b[0:1])
		}
		b = b[1:]
	}
	return
}

func (p *telnetProtocol) sendCommand(cmd ...byte) (err error) {
	cmd = append([]byte{InterpretAsCommand}, cmd...)
	_, err = p.out.Write(cmd)
	return
}

type readerState func(*telnetProtocol, byte) (readerState, bool)

func readAscii(_ *telnetProtocol, c byte) (readerState, bool) {
	if c == InterpretAsCommand {
		return readCommand, false
	}
	return readAscii, true
}

func readCommand(_ *telnetProtocol, c byte) (readerState, bool) {
	switch c {
	case InterpretAsCommand:
		return readAscii, true
	case Do:
		return readDoOption, false
	case Dont:
		return readDontOption, false
	case Will:
		return readWillOption, false
	case Wont:
		return readWontOption, false
	}
	return readAscii, false
}

func readDoOption(p *telnetProtocol, c byte) (readerState, bool) {
	p.sendCommand(Wont, c)
	return readAscii, false
}

func readDontOption(p *telnetProtocol, c byte) (readerState, bool) {
	p.sendCommand(Wont, c)
	return readAscii, false
}

func readWillOption(p *telnetProtocol, c byte) (readerState, bool) {
	p.sendCommand(Dont, c)
	return readAscii, false
}

func readWontOption(p *telnetProtocol, c byte) (readerState, bool) {
	p.sendCommand(Dont, c)
	return readAscii, false
}
