package main

import (
	"io"
	"sync"

	"github.com/stesla/telnet"
)

type Proxy interface {
	io.Writer

	AddDownstream(telnet.Conn)
}

var proxiesMutex sync.Mutex
var proxies = make(map[string]*proxyImpl)

func ConnectProxy(key string, conn telnet.Conn, addr string, toSend []byte) (Proxy, error) {
	proxy, isNew := findProxyByKey(key)
	proxy.AddDownstream(conn)
	if isNew {
		if err := proxy.connect(addr); err != nil {
			return nil, err
		}
		if _, err := proxy.Write(toSend); err != nil {
			return nil, err
		}
		go proxy.runForever(key)
	}
	return proxy, nil
}

type proxyImpl struct {
	mux         sync.Mutex
	upstream    telnet.Conn
	downstreams []telnet.Conn
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

func (p *proxyImpl) AddDownstream(conn telnet.Conn) {
	p.mux.Lock()
	defer p.mux.Unlock()
	p.downstreams = append(p.downstreams, conn)
}

func (p *proxyImpl) Write(bytes []byte) (int, error) {
	return p.upstream.Write(bytes)
}

func (p *proxyImpl) connect(addr string) (err error) {
	p.upstream, err = telnet.Dial(addr)
	return
}

func (p *proxyImpl) runForever(key string) {
	defer removeProxyByKey(key)
	defer func() {
		p.upstream.Close()
		for _, downstream := range p.downstreams {
			downstream.Close()
		}
	}()
	for {
		var buf = make([]byte, 4096)
		n, err := p.upstream.Read(buf)
		if err != nil {
			return
		}
		buf = buf[:n]
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
}