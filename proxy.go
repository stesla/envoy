package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stesla/telnet"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
)

const historySize = 20 * 1024 // about 256 lines of text
const logSepFormat = "2006-01-02 15:04:05 -0700 MST"

var proxiesMutex sync.Mutex
var proxies = make(map[string]*Proxy)

func CloseAll() {
	proxiesMutex.Lock()
	defer proxiesMutex.Unlock()
	for _, proxy := range proxies {
		proxy.Close()
	}
}

func ReopenLogFiles() {
	proxiesMutex.Lock()
	defer proxiesMutex.Unlock()
	for _, proxy := range proxies {
		proxy.reopenLog()
	}
}

func ProxyForKey(key string) *Proxy {
	proxiesMutex.Lock()
	defer proxiesMutex.Unlock()
	_, found := proxies[key]
	if !found {
		proxies[key] = &Proxy{key: key}
	}
	return proxies[key]
}

func removeProxyByKey(key string) {
	proxiesMutex.Lock()
	defer proxiesMutex.Unlock()
	delete(proxies, key)
}

type Proxy struct {
	key         string
	mux         sync.Mutex
	upstreamLog io.WriteCloser
	log         *logrusLogger
	upstream    telnet.Conn
	downstreams []io.WriteCloser

	allowCharsetWithoutBinary bool
	forceSuppressGoAhead      bool
	upstreamEncoding          encoding.Encoding
}

func (p *Proxy) AddDownstream(downstream io.WriteCloser) {
	p.mux.Lock()
	defer p.mux.Unlock()
	p.downstreams = append(p.downstreams, downstream)
}

func (p *Proxy) Close() error {
	p.closeUpstream()
	p.closeDownstreams()
	p.closeLog()
	return nil
}

func (p *Proxy) Initialize(addr string, toSend []byte) error {
	if err := p.openLog(); err != nil {
		return err
	}
	if err := p.connect(addr); err != nil {
		return err
	}
	if _, err := p.Write(toSend); err != nil {
		return err
	}
	go p.runForever()
	return nil
}

func (p *Proxy) IsNew() bool {
	return p.upstream == nil
}

func (p *Proxy) Read(bytes []byte) (int, error) {
	return p.upstream.Read(bytes)
}

func (p *Proxy) SetOption(option string, raw string) error {
	switch option {
	case "allow_charset_without_binary":
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		p.allowCharsetWithoutBinary = value
	case "encoding":
		enc, err := ianaindex.IANA.Encoding(raw)
		if err != nil {
			return err
		}
		p.upstreamEncoding = enc
	case "force_suppress_go_ahead":
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		p.forceSuppressGoAhead = value
	}
	return nil
}

func (p *Proxy) Write(bytes []byte) (int, error) {
	return p.upstream.Write(bytes)
}

func (p *Proxy) WriteHistoryTo(w io.Writer) error {
	buf, err := p.readHistory()
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

func (p *Proxy) connect(addr string) (err error) {
	p.log = newLogrusLogger(log, logrus.Fields{
		"type": "server",
		"peer": addr,
	})
	tcpconn, err := net.Dial("tcp", addr)
	p.upstream = telnet.Client(p.log.traceConnection(tcpconn))
	if err != nil {
		return
	}
	p.upstream.SetLogger(p.log)
	p.negotiateOptions()
	return
}

func (p *Proxy) closeDownstreams() {
	p.mux.Lock()
	defer p.mux.Unlock()
	for _, downstream := range p.downstreams {
		downstream.Close()
	}
}

func (p *Proxy) closeLog() {
	p.mux.Lock()
	defer p.mux.Unlock()
	t := time.Now()
	fmt.Fprintf(p.upstreamLog, "--------------- closed - %s ---------------\n", t.Format(logSepFormat))
	p.upstreamLog.Close()
}

func (p *Proxy) closeUpstream() {
	p.mux.Lock()
	defer p.mux.Unlock()
	p.upstream.Close()
}

func (p *Proxy) logFileName() string {
	return path.Join(
		*logdir,
		fmt.Sprintf("%s-%s.log", time.Now().Format("2006-01-02"), p.key),
	)
}

func (p *Proxy) negotiateOptions() {
	options := []telnet.Option{
		telnet.NewTransmitBinaryOption(),
		telnet.NewCharsetOption(!p.allowCharsetWithoutBinary),
	}
	if !p.forceSuppressGoAhead {
		options = append(options, telnet.NewSuppressGoAheadOption())
	}
	for _, opt := range options {
		opt.Allow(true, true)
		p.upstream.BindOption(opt)
	}

	if p.upstreamEncoding != nil {
		p.upstream.AddListener("update-option", telnet.FuncListener{
			Func: func(data any) {
				switch t := data.(type) {
				case telnet.UpdateOptionEvent:
					switch opt := t.Option; opt.Byte() {
					case telnet.Charset:
						if t.WeChanged && opt.EnabledForUs() {
							p.upstream.RequestEncoding(p.upstreamEncoding)
						}
					}
				}
			},
		})
	}

	if p.forceSuppressGoAhead {
		p.upstream.SuppressGoAhead(true)
	} else {
		p.upstream.EnableOptionForThem(telnet.SuppressGoAhead, true)
		p.upstream.EnableOptionForUs(telnet.SuppressGoAhead, true)
	}

	p.upstream.EnableOptionForThem(telnet.TransmitBinary, true)
	p.upstream.EnableOptionForUs(telnet.TransmitBinary, true)

	p.upstream.EnableOptionForThem(telnet.Charset, true)
	p.upstream.EnableOptionForUs(telnet.Charset, true)
}

const bannerLogOpened = "--------------- opened"

func (p *Proxy) openLog() error {
	log, err := os.OpenFile(
		p.logFileName(),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return err
	}
	p.mux.Lock()
	p.upstreamLog = log
	p.mux.Unlock()
	t := time.Now()
	fmt.Fprintf(p.upstreamLog, bannerLogOpened+" - %s ---------------\n", t.Format(logSepFormat))
	return err
}

func (p *Proxy) readHistory() ([]byte, error) {
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
	for {
		i := strings.Index(string(buf), bannerLogOpened)
		if i > 0 {
			buf = buf[i:]
			i := strings.Index(string(buf), "\n")
			buf = buf[i+1:]
		} else {
			break
		}
	}
	return buf, nil
}

func (p *Proxy) reopenLog() error {
	p.closeLog()
	return p.openLog()
}

func (p *Proxy) runForever() {
	defer removeProxyByKey(p.key)
	defer p.Close()
	for {
		var buf = make([]byte, 4096)
		n, err := p.Read(buf)
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

func (p *Proxy) sendDownstream(buf []byte) {
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

func (p *Proxy) writeLog(buf []byte) (int, error) {
	p.mux.Lock()
	defer p.mux.Unlock()
	return p.upstreamLog.Write(buf)
}
