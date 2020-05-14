package proxy

import (
	"bufio"
	"bytes"
	"envoy/telnet"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	homedir "github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

const motd = `
Welcome to Envoy
------------------------------------------------------------------------
  "connect <name> <password>" connects you to an existing world.
------------------------------------------------------------------------
`

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

func StartSession(conn telnet.Conn) {
	conn.LogEntry().Println("connected")
	conn.NegotiateOptions()

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
	proxyName := strings.ToLower(words[1])
	proxy, found := findProxyByName(proxyName)

	if !found || words[2] != viper.GetString("password") {
		fmt.Fprintln(conn, "invalid proxy name or password")
		conn.Close()
		return
	}

	err = proxy.AddClient(&client{r: r, Conn: conn})
	if err != nil {
		msg := fmt.Sprintf("error connecting to world '%s': %v", proxy.Name, err)
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
}

func (c *client) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

type ioresult struct {
	n   int
	err error
}

type proxy struct {
	Name      string
	Password  string
	Address   string
	Log       string
	OnConnect string

	addClient   chan addClientReq
	close       chan chan error
	reopenLog   chan chan error
	writeServer chan writereq
	writeClient chan writereq
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

func (p *proxy) ReopenLog() error {
	ch := make(chan error)
	p.reopenLog <- ch
	return <-ch
}

func (p *proxy) ServerWriter() io.Writer {
	return &writereqWriter{p.writeServer}
}

func (p *proxy) connect() (conn telnet.Conn, err error) {
	conn, err = telnet.Dial(p.Address)
	if err != nil {
		return
	}
	conn.LogEntry().Println("connected")

	if p.OnConnect != "" {
		_, err = fmt.Fprintln(conn, p.OnConnect)
	}

	return
}

func (p *proxy) loop() {
	defer proxies.Delete(p.Name)

	var clients = make(map[io.Writer]struct{})
	deleteClient := func(w io.Writer) {
		delete(clients, w)
		if c, ok := w.(io.Closer); ok {
			c.Close()
		}
	}

	var history = newHistory()
	clients[history] = struct{}{}

	var awaitClientNegotiation = make(chan *client, 1)
	var logFile *logFile
	var server telnet.Conn
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
			logFile.Close()
			ch <- server.Close()
			server.LogEntry().Println("disconnected")
			return

		case ch := <-p.reopenLog:
			var err error
			if logFile != nil {
				logFile.Close()
				logFile, err = openLogFile(p.Log)
			}
			ch <- err

		case client := <-awaitClientNegotiation:
			clients[client] = struct{}{}
			history.WriteTo(client)

		case req := <-p.addClient:
			if server == nil {
				var err error
				server, err = p.connect()
				if err != nil {
					req.ch <- err
					break
				}
				if p.Log != "" {
					logFile, err = openLogFile(p.Log)
					if err != nil {
						req.ch <- err
						break
					}
				}
				server.NegotiateOptions()
				writeServer = p.writeServer
			}
			go func(client *client) {
				if await := server.AwaitNegotiation(); await != nil {
					<-await
				}
				if await := client.AwaitNegotiation(); await != nil {
					<-await
				}
				awaitClientNegotiation <- client
			}(req.c)
			go func() {
				io.Copy(p.ServerWriter(), req.c)
				req.c.LogEntry().Println("disconnected")
			}()
			close(req.ch)

		case req := <-p.writeClient:
			for c, _ := range clients {
				nw, ew := c.Write(req.buf)
				if ew != nil || nw != len(req.buf) {
					deleteClient(c)
				}
			}
			if logFile != nil {
				logFile.Write(req.buf)
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

func findProxyByName(name string) (*proxy, bool) {
	obj, found := proxies.Load(name)
	if found {
		p, ok := obj.(*proxy)
		return p, ok
	}

	h := viper.GetStringMapString("proxies." + name)
	if len(h) == 0 {
		return nil, false
	}

	p := &proxy{
		Name:      name,
		Address:   h["address"],
		Log:       h["log"],
		OnConnect: h["onconnect"],

		addClient:   make(chan addClientReq),
		close:       make(chan chan error, 1),
		reopenLog:   make(chan chan error),
		writeServer: make(chan writereq),
		writeClient: make(chan writereq),
	}
	go p.loop()
	proxies.Store(name, p)
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
		if i := bytes.IndexRune(h.buf, '\n'); i >= 0 {
			h.buf = h.buf[i+1:]
		}
	}
	return len(p), nil
}

func (h *history) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(h.buf)
	return int64(n), err
}

type logFile struct {
	name string
	*os.File
}

func openLogFile(name string) (*logFile, error) {
	t := time.Now()
	filename, err := homedir.Expand(t.Format(name))
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(f, "--------------- opened - %s ---------------\n", t.Format(logSepFormat))
	return &logFile{name: name, File: f}, nil
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
