package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stesla/telnet"
	"golang.org/x/text/encoding/unicode"
)

type Proxy interface {
	io.Writer

	AddDownstream(io.WriteCloser)
}

var proxiesMutex sync.Mutex
var proxies = make(map[string]*proxyImpl)

func ConnectProxy(key string, conn telnet.Conn, addr string, toSend []byte) (Proxy, error) {
	proxy, isNew := findProxyByKey(key)
	if isNew {
		if log, err := openLogFile(key); err != nil {
			return nil, err
		} else {
			proxy.log = log
		}

		if err := proxy.connect(addr); err != nil {
			return nil, err
		}

		if _, err := proxy.Write(toSend); err != nil {
			return nil, err
		}
		go proxy.runForever(key)
	}
	proxy.AddDownstream(conn)
	return proxy, nil
}

func openLogFile(key string) (io.WriteCloser, error) {
	timestr := time.Now().Format("2006-01-02")
	name := fmt.Sprintf("%s-%s.log", timestr, key)
	return os.OpenFile(
		path.Join(*logdir, name),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
}

type proxyImpl struct {
	mux         sync.Mutex
	log         io.WriteCloser
	upstream    telnet.Conn
	downstreams []io.WriteCloser
}

func findProxyByKey(key string) (*proxyImpl, bool) {
	proxiesMutex.Lock()
	defer proxiesMutex.Unlock()
	_, found := proxies[key]
	if !found {
		proxies[key] = &proxyImpl{}
	}
	return proxies[key], !found
}

func removeProxyByKey(key string) {
	proxiesMutex.Lock()
	defer proxiesMutex.Unlock()
	delete(proxies, key)
}

func (p *proxyImpl) AddDownstream(downstream io.WriteCloser) {
	p.mux.Lock()
	defer p.mux.Unlock()
	p.downstreams = append(p.downstreams, downstream)
}

func (p *proxyImpl) Write(bytes []byte) (int, error) {
	return p.upstream.Write(bytes)
}

func (p *proxyImpl) connect(addr string) (err error) {
	p.upstream, err = telnet.Dial(addr)
	if err != nil {
		return
	}
	p.upstream.SetLogger(newLogrusLogger(log, logrus.Fields{
		"type": "server",
		"peer": addr,
	}))
	p.negotiateOptions()
	return
}

func (p *proxyImpl) Close() error {
	p.mux.Lock()
	defer p.mux.Unlock()
	p.upstream.Close()
	for _, downstream := range p.downstreams {
		downstream.Close()
	}
	p.log.Close()
	return nil
}

func (p *proxyImpl) negotiateOptions() {
	for _, opt := range []telnet.Option{
		telnet.NewSuppressGoAheadOption(),
		telnet.NewTransmitBinaryOption(),
		telnet.NewCharsetOption(),
	} {
		opt.Allow(true, true)
		p.upstream.BindOption(opt)
	}

	p.upstream.AddListener("update-option", telnet.FuncListener{
		Func: func(data any) {
			switch t := data.(type) {
			case telnet.UpdateOptionEvent:
				switch opt := t.Option; opt.Byte() {
				case telnet.Charset:
					if t.WeChanged && opt.EnabledForUs() {
						p.upstream.RequestEncoding(unicode.UTF8)
					}
				}
			}
		},
	})

	p.upstream.EnableOptionForThem(telnet.SuppressGoAhead, true)
	p.upstream.EnableOptionForUs(telnet.SuppressGoAhead, true)

	p.upstream.EnableOptionForThem(telnet.TransmitBinary, true)
	p.upstream.EnableOptionForUs(telnet.TransmitBinary, true)

	p.upstream.EnableOptionForThem(telnet.Charset, true)
	p.upstream.EnableOptionForUs(telnet.Charset, true)
}

func (p *proxyImpl) runForever(key string) {
	defer removeProxyByKey(key)
	defer p.Close()
	for {
		var buf = make([]byte, 4096)
		n, err := p.upstream.Read(buf)
		if err != nil {
			return
		}
		buf = buf[:n]

		if _, err := p.writeLog(buf); err != nil {
			// if we can't write to the log, we don't want to receive any
			// more output from the server
			return
		}

		p.sendDownstream(buf)
	}
}

func (p *proxyImpl) sendDownstream(buf []byte) {
	p.mux.Lock()
	defer p.mux.Unlock()
	i := 0
	for _, downstream := range p.downstreams {
		if _, err := downstream.Write(buf); err == nil {
			p.downstreams[i] = downstream
			i++
		}
	}
	for j := i; j < len(p.downstreams); j++ {
		p.downstreams[j] = nil
	}
	p.downstreams = p.downstreams[:i]
}

func (p *proxyImpl) writeLog(buf []byte) (int, error) {
	p.mux.Lock()
	defer p.mux.Unlock()
	return p.log.Write(buf)
}
