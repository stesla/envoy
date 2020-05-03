package cmd

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	proxies  = &sync.Map{}
	startCmd = &cobra.Command{
		Use:   "start",
		Short: "start proxy server",
		Run:   start,
	}
)

func init() {
	startCmd.PersistentFlags().StringP("listen", "l", ":4001", "address to listen on")
	viper.BindPFlag("listen", startCmd.PersistentFlags().Lookup("listen"))
	rootCmd.AddCommand(startCmd)
}

func readProxies() (out map[string]*proxy) {
	out = make(map[string]*proxy)
	for name, _ := range viper.GetStringMapString("proxies") {
		p := viper.GetStringMapString("proxies." + name)
		out[name] = &proxy{
			Name:      name,
			Password:  p["password"],
			Address:   p["address"],
			Log:       p["log"],
			OnConnect: p["onconnect"],

			addClient:   make(chan addClientReq),
			close:       make(chan chan error),
			writeServer: make(chan writereq),
			writeClient: make(chan writereq),
		}
		go out[name].loop()
	}
	return
}

func start(cmd *cobra.Command, args []string) {
	for k, v := range readProxies() {
		proxies.Store(k, v)
	}

	addr := viper.GetString("listen")
	log.Printf("listening on '%s'", addr)

	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}

		go startsession(conn)
	}
}

func startsession(conn net.Conn) {
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
	obj, found := proxies.Load(proxyName)
	proxy := obj.(*proxy)

	if !found || words[2] != proxy.Password {
		fmt.Fprintln(conn, "invalid proxy name or password")
		conn.Close()
		return
	}

	err = proxy.AddClient(&client{Reader: r, WriteCloser: conn})
	if err != nil {
		msg := fmt.Sprintf("error connecting to world '%s': %v", proxy.Name, err)
		log.Println(msg)
		fmt.Fprintln(conn, msg)
		conn.Close()
		return
	}
}

type addClientReq struct {
	c  io.ReadWriteCloser
	ch chan error
}

type client struct {
	io.Reader
	io.WriteCloser
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
	writeServer chan writereq
	writeClient chan writereq
}

func (p *proxy) AddClient(c io.ReadWriteCloser) error {
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

func (p *proxy) ServerWriter() io.Writer {
	return &writereqWriter{p.writeServer}
}

func (p *proxy) connect() (conn net.Conn, log *os.File, err error) {
	conn, err = net.Dial("tcp", p.Address)
	if err != nil {
		return
	}

	if p.Log != "" {
		log, err = p.openLog()
		if err != nil {
			return
		}
	}

	if p.OnConnect != "" {
		_, err = fmt.Fprintln(conn, p.OnConnect)
	}

	return
}

func (p *proxy) loop() {
	var clients = make(map[io.WriteCloser]struct{})
	var server net.Conn
	var readServer chan struct{}
	var readServerDone chan struct{}
	var writeClient = p.writeClient
	var writeServer = p.writeServer
	var writeServerDone chan struct{}
	for {
		if readServerDone == nil && len(clients) > 0 {
			readServer = make(chan struct{}, 1)
			readServer <- struct{}{}
		}

		select {
		case ch := <-p.close:
			for c, _ := range clients {
				delete(clients, c)
				c.Close()
			}
			ch <- server.Close()
			server = nil
			readServer = nil
			readServerDone = nil
			writeClient = p.writeClient
			writeServer = p.writeServer
			writeServerDone = nil

		case req := <-p.addClient:
			if server == nil {
				conn, log, err := p.connect()
				if err != nil {
					req.ch <- err
					break
				}
				server = conn
				logr, logw := io.Pipe()
				clients[logw] = struct{}{}
				go func() {
					io.Copy(log, noTelnet(logr))
					log.Close()
				}()
			}
			close(req.ch)
			clients[req.c] = struct{}{}
			go io.Copy(p.ServerWriter(), req.c)

		case req := <-writeClient:
			for c, _ := range clients {
				nw, ew := c.Write(req.buf)
				if ew != nil || nw != len(req.buf) {
					delete(clients, c)
					c.Close()
				}
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

func (p *proxy) openLog() (*os.File, error) {
	t := time.Now()
	filename, err := homedir.Expand(t.Format(p.Log))
	if err != nil {
		return nil, err
	}
	return os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 644)
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
