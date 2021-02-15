package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/stesla/envoy/telnet"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
)

const motd = `
Welcome to Envoy
------------------------------------------------------------------------
  "connect <name> <password>" connects you to an existing world.
------------------------------------------------------------------------
`

func init() {
	viper.SetDefault("proxy.log.dir", "~/rplogs/auto")
	viper.SetDefault("proxy.log.namefmt", "2006-01-02_$key.log")
}

func CloseAll() {
	proxies.Range(func(key, value interface{}) bool {
		log.Infof("closing proxy '%v'", key)
		value.(*proxy).Close()
		return true
	})
}

func ReopenLogs() {
	proxies.Range(func(key, value interface{}) bool {
		log.Infof("reopening log for '%v'", key)
		value.(*proxy).ReopenLog()
		return true
	})
}

func StartSession(conn telnet.Conn, logEntry *log.Entry) {
	logEntry.Println("connected")

	fmt.Fprintln(conn, motd)

	r := bufio.NewReader(conn)

	line, err := r.ReadString('\n')
	if err != nil {
		conn.Close()
		return
	}
	words := strings.Split(strings.TrimSpace(line), " ")
	if len(words) != 3 || words[0] != "connect" {
		conn.Close()
		return
	}
	key := strings.ToLower(words[1])
	proxy, found := findProxyByKey(key)

	if !found || words[2] != viper.GetString("password") {
		fmt.Fprintln(conn, "invalid proxy name or password")
		conn.Close()
		return
	}

	err = proxy.AddClient(&client{r: r, Conn: conn, log: logEntry})
	if err != nil {
		msg := fmt.Sprintf("error connecting to world %q: %v", proxy.Key, err)
		log.Println(msg)
		fmt.Fprintln(conn, msg)
		conn.Close()
		return
	}
}

type addClientReq struct {
	c  *client
	ch chan error
}

type client struct {
	r io.Reader
	telnet.Conn
	log *log.Entry
}

func (c *client) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

type ioresult struct {
	n   int
	err error
}

type proxy struct {
	Name           string
	Key            string
	Password       string
	Address        string
	Log            bool
	Raw            bool
	OnConnect      string
	ServerEncoding string

	addClient   chan addClientReq
	close       chan chan error
	reopenLog   chan chan error
	writeServer chan writereq
	writeClient chan writereq

	log *log.Entry
}

func (p *proxy) AddClient(c *client) error {
	ch := make(chan error)
	p.addClient <- addClientReq{c, ch}
	return <-ch
}

func (p *proxy) ClientWriter() io.Writer {
	return &writereqWriter{p.writeClient}
}

func (p *proxy) Close() error {
	ch := make(chan error)
	p.close <- ch
	return <-ch
}

func (p *proxy) ConnectString() string {
	if p.OnConnect != "" {
		return p.OnConnect
	} else if p.Name != "" && p.Password != "" {
		return fmt.Sprintf("connect \"%s\" %s", p.Name, p.Password)
	}
	return ""
}

func (p *proxy) LogFileName() string {
	homedir := viper.GetString("user.home")
	dir := strings.Replace(viper.GetString("proxy.log.dir"), "~", homedir, 1)
	fmt := strings.Replace(viper.GetString("proxy.log.namefmt"), "$key", p.Key, -1)
	filename := path.Join(dir, fmt)
	t := time.Now()
	return t.Format(filename)
}

func (p *proxy) ReopenLog() error {
	ch := make(chan error)
	p.reopenLog <- ch
	return <-ch
}

func (p *proxy) ServerWriter() io.Writer {
	return &writereqWriter{p.writeServer}
}

func (p *proxy) connect() (server telnet.Conn, err error) {
	var enc encoding.Encoding
	if p.ServerEncoding != "" {
		// These strings get validated in cmd/root.go when the config is
		// initially loaded, so there will not be an error here.
		enc, _ = ianaindex.IANA.Encoding(p.ServerEncoding)
	}

	conn, err := net.Dial("tcp", p.Address)
	if err != nil {
		return nil, err
	}
	p.log = log.WithFields(log.Fields{
		"type": telnet.ServerType,
		"addr": p.Address,
	})

	server = telnet.Wrap(telnet.ServerType, conn)
	server.SetLog(p.log)
	server.GetOption(telnet.Charset).Allow(true, true)
	server.GetOption(telnet.EndOfRecord).Allow(true, true)
	server.GetOption(telnet.SuppressGoAhead).Allow(true, true)
	server.GetOption(telnet.TransmitBinary).Allow(true, true)

	if enc != nil {
		server.SetEncoding(enc)
	}
	p.log.Println("connected")

	if str := p.ConnectString(); str != "" {
		_, err = fmt.Fprintln(conn, str)
	}

	return
}

func (p *proxy) loop(key string) {
	defer proxies.Delete(key)

	var clients = make(map[io.Writer]struct{})
	deleteClient := func(w io.Writer) {
		delete(clients, w)
		if c, ok := w.(io.Closer); ok {
			c.Close()
		}
	}

	var history = newHistory()
	clients[history] = struct{}{}

	var log *logFile
	var server telnet.Conn
	var rawLog *logFile
	var readServer chan struct{}
	var readServerDone chan struct{}
	var writeServer chan writereq
	var writeServerDone chan struct{}
	for {
		if server != nil && readServerDone == nil {
			readServer = make(chan struct{}, 1)
			readServer <- struct{}{}
		}

		select {
		case ch := <-p.close:
			for c, _ := range clients {
				deleteClient(c)
			}
			if log != nil {
				log.Close()
			}
			if rawLog != nil {
				rawLog.Close()
			}
			ch <- server.Close()
			p.log.Println("disconnected")
			return

		case ch := <-p.reopenLog:
			var err error
			name := p.LogFileName()
			if log != nil {
				log.Close()
				log, err = openLogFile(name)
			}
			if err == nil && rawLog != nil {
				rawLog.Close()
				rawLog, err = openLogFile(name + ".raw")
			}
			ch <- err

		case req := <-p.addClient:
			if server == nil {
				var err error
				server, err = p.connect()
				if err != nil {
					req.ch <- err
					break
				}
				if p.Log {
					var logName = p.LogFileName()
					log, err = openLogFile(logName)
					if err != nil {
						req.ch <- err
						break
					}
					if p.Raw {
						rawLog, err = openLogFile(logName + ".raw")
						if err != nil {
							req.ch <- err
							break
						}
						server.SetRawLogWriter(rawLog)
					}
				}

				server.RegisterHandler(&telnet.CharsetOption{})
				server.GetOption(telnet.Charset).EnableThem()
				server.GetOption(telnet.Charset).EnableUs()
				writeServer = p.writeServer
			}
			go func() {
				io.Copy(p.ServerWriter(), req.c)
				req.c.log.Println("disconnected")
			}()
			clients[req.c] = struct{}{}
			history.WriteTo(req.c)
			close(req.ch)

		case req := <-p.writeClient:
			for c, _ := range clients {
				nw, ew := c.Write(req.buf)
				if ew != nil || nw != len(req.buf) {
					deleteClient(c)
				}
			}
			if log != nil {
				log.Write(req.buf)
			}
			req.ch <- ioresult{len(req.buf), nil}

		case req := <-writeServer:
			writeServer = nil
			writeServerDone = make(chan struct{})
			go func() {
				nw, ew := server.Write(req.buf)
				req.ch <- ioresult{nw, ew}
				writeServerDone <- struct{}{}
			}()

		case <-writeServerDone:
			writeServerDone = nil
			writeServer = p.writeServer

		case <-readServer:
			readServer = nil
			readServerDone = make(chan struct{})
			go func() {
				buf := make([]byte, 1024)
				nr, er := server.Read(buf)
				p.ClientWriter().Write(buf[:nr])
				if er != nil {
					p.Close()
					return
				}
				readServerDone <- struct{}{}
			}()

		case <-readServerDone:
			readServerDone = nil
		}
	}
}

const logSepFormat = "2006-01-02 15:04:05 -0700 MST"

var proxies = &sync.Map{}

func findProxyByKey(key string) (*proxy, bool) {
	obj, found := proxies.Load(key)
	if found {
		p, ok := obj.(*proxy)
		return p, ok
	}

	h := viper.GetStringMapString("proxies." + key)
	if len(h) == 0 {
		return nil, false
	}

	p := &proxy{
		Key:       key,
		Name:      h["name"],
		Address:   h["address"],
		Log:       true,
		Raw:       false,
		OnConnect: h["onconnect"],
		Password:  h["password"],

		addClient:   make(chan addClientReq),
		close:       make(chan chan error, 1),
		reopenLog:   make(chan chan error),
		writeServer: make(chan writereq),
		writeClient: make(chan writereq),
	}
	if log := "proxies." + key + ".log"; viper.IsSet(log) {
		p.Log = viper.GetBool(log)
	}
	if raw := "proxies." + key + ".raw"; viper.IsSet(raw) {
		p.Raw = viper.GetBool(raw)
	}
	if encoding := "proxies." + key + ".encoding"; viper.IsSet(encoding) {
		p.ServerEncoding = viper.GetString(encoding)
	}
	proxies.Store(key, p)
	go p.loop(key)
	return p, true
}

const (
	defaultHistorySize = 40 * 1024 // about 512 lines of text
	defaultScrollSize  = 10 * 1024 // about 128 lines of text
)

type history struct {
	n, s int
	buf  []byte
}

func newHistory() *history {
	return newHistoryWithSize(defaultHistorySize, defaultScrollSize)
}

func newHistoryWithSize(size, scroll int) *history {
	return &history{n: size, s: scroll, buf: make([]byte, 0, size)}
}

func (h *history) Write(p []byte) (int, error) {
	if l := len(h.buf) + len(p); l <= h.n {
		// it fits, no problem
		h.buf = append(h.buf, p...)
	} else {
		for l > h.n {
			l -= h.s
		}
		if n := l - len(p); n <= 0 {
			h.buf = p[len(p)-l:]
		} else {
			h.buf = append(h.buf[len(h.buf)-n:], p...)
		}
	}
	return len(p), nil
}

func (h *history) WriteTo(w io.Writer) (int64, error) {
	var buf []byte
	if i := bytes.IndexRune(h.buf, '\n'); i >= 0 {
		buf = h.buf[i+1:]
	}
	n, err := w.Write(buf)
	return int64(n), err
}

type logFile struct {
	name string
	*os.File
}

func openLogFile(filename string) (*logFile, error) {
	t := time.Now()
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(f, "--------------- opened - %s ---------------\n", t.Format(logSepFormat))
	return &logFile{name: filename, File: f}, nil
}

func (l *logFile) Close() error {
	if l.File == nil {
		return nil
	}
	t := time.Now()
	fmt.Fprintf(l.File, "--------------- closed - %s ---------------\n", t.Format(logSepFormat))
	l.File.Sync()
	return l.File.Close()
}

type telnetFilter struct {
	r io.Reader
	s telnetState
}

const (
	telnetWILL = 251 + iota
	telnetWONT
	telnetDO
	telnetDONT
	telnetIAC
)

func noTelnet(r io.Reader) io.Reader {
	return &telnetFilter{r, telnetStateNormal}
}

func (st *telnetFilter) Read(p []byte) (int, error) {
	q := make([]byte, len(p))
	nr, er := st.r.Read(q)
	if er != nil {
		return nr, er
	}
	var n int
	for _, c := range q[0:nr] {
		var ok bool
		st.s, ok = st.s(c)
		if ok {
			p[n] = c
			n++
		}
	}
	return n, nil
}

type telnetState func(byte) (telnetState, bool)

func telnetStateNormal(c byte) (telnetState, bool) {
	switch c {
	case telnetIAC:
		return telnetStateIAC, false
	default:
		return telnetStateNormal, true
	}
}

func telnetStateIAC(c byte) (telnetState, bool) {
	switch c {
	case telnetWILL, telnetWONT, telnetDO, telnetDONT:
		return telnetStateOption, false
	case telnetIAC:
		return telnetStateNormal, true
	default:
		return telnetStateNormal, false
	}
}

func telnetStateOption(c byte) (telnetState, bool) {
	return telnetStateNormal, false
}

type writereq struct {
	buf []byte
	ch  chan<- ioresult
}

type writereqWriter struct {
	ch chan<- writereq
}

func (w *writereqWriter) Write(data []byte) (int, error) {
	ch := make(chan ioresult)
	w.ch <- writereq{data, ch}
	r := <-ch
	return r.n, r.err
}
