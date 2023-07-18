package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stesla/telnet"
	"golang.org/x/text/encoding/unicode"
)

const historySize = 20 * 1024 // about 256 lines of text
const logSepFormat = "2006-01-02 15:04:05 -0700 MST"

type Proxy interface {
	io.Writer

	AddDownstream(io.WriteCloser)
}

var proxiesMutex sync.Mutex
var proxies = make(map[string]*proxyImpl)

func CloseAll() {
	proxiesMutex.Lock()
	defer proxiesMutex.Unlock()
	for _, proxy := range proxies {
		proxy.Close()
	}
}

func ConnectProxy(key string, conn telnet.Conn, addr string, toSend []byte) (Proxy, error) {
	proxy, isNew := findProxyByKey(key)
	if isNew {
		if err := proxy.openLog(); err != nil {
			return nil, err
		}
		if err := proxy.connect(addr); err != nil {
			return nil, err
		}
		if _, err := proxy.Write(toSend); err != nil {
			return nil, err
		}
		go proxy.runForever()
	} else {
		buf, err := proxy.readHistory()
		if err != nil {
			return nil, err
		}
		_, err = conn.Write(buf)
		if err != nil {
			return nil, err
		}
	}
	proxy.AddDownstream(conn)
	return proxy, nil
}

func ReopenLogFiles() {
	proxiesMutex.Lock()
	defer proxiesMutex.Unlock()
	for _, proxy := range proxies {
		proxy.reopenLog()
	}
}

type proxyImpl struct {
	key         string
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
		proxies[key] = &proxyImpl{key: key}
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
	p.closeUpstream()
	p.closeDownstreams()
	p.closeLog()
	return nil
}

func (p *proxyImpl) closeDownstreams() {
	p.mux.Lock()
	defer p.mux.Unlock()
	for _, downstream := range p.downstreams {
		downstream.Close()
	}
}

func (p *proxyImpl) closeLog() {
	p.mux.Lock()
	defer p.mux.Unlock()
	t := time.Now()
	fmt.Fprintf(p.log, "--------------- closed - %s ---------------\n", t.Format(logSepFormat))
	p.log.Close()
}

func (p *proxyImpl) closeUpstream() {
	p.mux.Lock()
	defer p.mux.Unlock()
	p.upstream.Close()
}

func (p *proxyImpl) logFileName() string {
	return path.Join(
		*logdir,
		fmt.Sprintf("%s-%s.log", time.Now().Format("2006-01-02"), p.key),
	)
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

const bannerLogOpened = "--------------- opened"

func (p *proxyImpl) openLog() error {
	log, err := os.OpenFile(
		p.logFileName(),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return err
	}
	p.mux.Lock()
	p.log = log
	p.mux.Unlock()
	t := time.Now()
	fmt.Fprintf(p.log, bannerLogOpened+" - %s ---------------\n", t.Format(logSepFormat))
	return err
}

func (p *proxyImpl) readHistory() ([]byte, error) {
	file, err := os.Open(p.logFileName())
	if err != nil {
		return nil, err
	}
	end, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, err
	}
	if end > historySize {
		_, err = file.Seek(end-historySize, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
	}
	buf := make([]byte, historySize)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}
	buf = buf[:n]
	log.Print("read history: len(buf) before:", len(buf))
	for {
		i := strings.Index(string(buf), bannerLogOpened)
		log.Print("read history: i = ", i)
		if i > 0 {
			buf = buf[i:]
			i := strings.Index(string(buf), "\n")
			buf = buf[i+1:]
		} else {
			break
		}
	}
	log.Print("read history: len(buf) after:", len(buf))
	return buf, nil
}

func (p *proxyImpl) reopenLog() error {
	p.closeLog()
	return p.openLog()
}

func (p *proxyImpl) runForever() {
	defer removeProxyByKey(p.key)
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
