package cmd

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"

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

func ReadProxies() (out map[string]*proxy) {
	out = make(map[string]*proxy)
	for name, _ := range viper.GetStringMapString("proxies") {
		p := viper.GetStringMapString("proxies." + name)
		out[name] = &proxy{
			Name:      name,
			Password:  p["password"],
			Address:   p["address"],
			Log:       p["log"],
			OnConnect: p["onconnect"],

			addClient: make(chan addClientReq),
			close:     make(chan chan error),
			write:     make(chan writeReq),
		}
		go out[name].loop()
	}
	return
}

func start(cmd *cobra.Command, args []string) {
	for k, v := range ReadProxies() {
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

type proxy struct {
	Name      string
	Password  string
	Address   string
	Log       string
	OnConnect string

	addClient chan addClientReq
	close     chan chan error
	write     chan writeReq
}

func (p *proxy) connect() (conn net.Conn, err error) {
	conn, err = net.Dial("tcp", p.Address)
	return
}

func (p *proxy) loop() {
	var clients = make(map[Client]struct{})
	var server net.Conn
	var readServer chan struct{}
	var readDone chan struct{}
	var writeClient = make(chan []byte)
	var writeServer = p.write
	var writeDone chan struct{}
	for {
		var err error

		if readDone == nil && len(clients) > 0 {
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

		case req := <-p.addClient:
			if server == nil {
				server, err = p.connect()
				if err != nil {
					req.ch <- err
					server = nil
					break
				}
			}
			close(req.ch)
			clients[req.c] = struct{}{}
			go io.Copy(p, req.c)

		case buf := <-writeClient:
			for c, _ := range clients {
				nw, ew := c.Write(buf)
				if ew != nil || nw != len(buf) {
					delete(clients, c)
					c.Close()
				}
			}

		case req := <-writeServer:
			writeServer = nil
			writeDone = make(chan struct{}, 1)
			go func() {
				nw, ew := server.Write(req.buf)
				req.ch <- ioresult{nw, ew}
				writeDone <- struct{}{}
			}()

		case <-writeDone:
			writeDone = nil
			writeServer = p.write

		case <-readServer:
			readServer = nil
			readDone = make(chan struct{}, 1)
			go func() {
				buf := make([]byte, 1024)
				nr, er := server.Read(buf)
				if er != nil {
					// TODO: notify clients server d/c'd
					p.Close()
					return
				}
				writeClient <- buf[:nr]
				readDone <- struct{}{}
			}()

		case <-readDone:
			readDone = nil
		}
	}
}

type Client interface {
	io.ReadWriteCloser
}

type addClientReq struct {
	c  Client
	ch chan error
}

func (p *proxy) AddClient(c Client) error {
	ch := make(chan error)
	p.addClient <- addClientReq{c, ch}
	return <-ch
}

func (p *proxy) Close() error {
	ch := make(chan error)
	p.close <- ch
	return <-ch
}

type writeReq struct {
	buf []byte
	ch  chan<- ioresult
}

type ioresult struct {
	n   int
	err error
}

func (p *proxy) Write(data []byte) (int, error) {
	ch := make(chan ioresult)
	p.write <- writeReq{data, ch}
	r := <-ch
	return r.n, r.err
}

type stripTelnet struct {
	r io.Reader
	s state
}

const (
	telnetWILL = 251 + iota
	telnetWONT
	telnetDO
	telnetDONT
	telnetIAC
)

func (st *stripTelnet) Read(p []byte) (int, error) {
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

type state func(byte) (state, bool)

func stateNormal(c byte) (state, bool) {
	switch c {
	case telnetIAC:
		return stateIAC, false
	default:
		return stateNormal, true
	}
}

func stateIAC(c byte) (state, bool) {
	switch c {
	case telnetWILL, telnetWONT, telnetDO, telnetDONT:
		return stateOption, false
	case telnetIAC:
		return stateNormal, true
	default:
		return stateNormal, false
	}
}

func stateOption(c byte) (state, bool) {
	return stateNormal, false
}

type client struct {
	io.Reader
	io.WriteCloser
}
