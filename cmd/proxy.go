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

func ReadProxies() (out map[string]*proxy) {
	out = make(map[string]*proxy)
	for name, _ := range viper.GetStringMapString("proxies") {
		p := viper.GetStringMapString("proxies." + name)
		out[name] = &proxy{
			Name:      name,
			Password:  p["password"],
			Server:    p["server"],
			Log:       p["log"],
			OnConnect: p["onconnect"],
		}
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

		go session(conn)
	}
}

func session(client net.Conn) {
	defer client.Close()

	r := bufio.NewReader(client)

	line, err := r.ReadString('\n')
	if err != nil {
		return
	}
	words := strings.Split(strings.TrimSpace(line), " ")
	if len(words) != 3 || words[0] != "connect" {
		return
	}
	proxyName := strings.ToLower(words[1])
	obj, found := proxies.Load(proxyName)
	proxy := *obj.(*proxy)

	if !found || words[2] != proxy.Password {
		fmt.Fprintln(client, "invalid proxy name or password")
		return
	}
	proxy.Serve(r, client)
}

type proxy struct {
	Name      string
	Password  string
	Server    string
	Log       string
	OnConnect string

	sync.Mutex

	conn net.Conn
	log  *logger

	sr io.Reader
	sw io.Writer
}

func (p *proxy) Connect() (first bool, err error) {
	p.Lock()
	defer p.Unlock()
	if p.conn != nil {
		return false, nil
	}
	first = true
	p.conn, err = net.Dial("tcp", p.Server)
	return
}

func (p *proxy) StartLog() (err error) {
	p.Lock()
	defer p.Unlock()
	if p.Log != "" && p.log == nil {
		p.log, err = OpenLog(p.Log, p.sr)
		p.sr = p.log
	}
	return
}

func (p *proxy) Serve(clientr io.Reader, clientw io.Writer) {
	first, err := p.Connect()
	if err != nil {
		fmt.Fprintln(clientw, "error connecting to server:", err)
	}
	p.sr, p.sw = p.conn, p.conn

	if first {
		if err := p.StartLog(); err != nil {
			fmt.Fprintln(clientw, "error opening log:", err)
		}
		_, err = fmt.Fprintln(p.sw, p.OnConnect)
		if err != nil {
			fmt.Fprintln(clientw, "error sending connect string:", err)
			return
		}
	}

	// send input to the server
	cc := make(chan bool)
	go func() {
		io.Copy(p.sw, clientr)
		close(cc)
	}()

	// send output to the client
	sc := make(chan bool)
	go func() {
		io.Copy(clientw, p.sr)
		close(sc)
	}()

	// wait until one direction closes, and then close the socket
	select {
	case <-cc:
	case <-sc:
		fmt.Fprintln(clientw, "connection closed by server")
	}
}

type logger struct {
	r io.Reader
	f *os.File
}

func OpenLog(namefmt string, r io.Reader) (*logger, error) {
	t := time.Now()
	filename, err := homedir.Expand(t.Format(namefmt))
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	log := &logger{&stripTelnet{r, stateNormal}, f}
	return log, nil
}

func (l *logger) Close() error {
	return l.f.Close()
}

func (l *logger) Read(p []byte) (int, error) {
	nr, er := l.r.Read(p)
	if nr > 0 {
		nw, ew := l.f.Write(p[0:nr])
		if ew != nil {
			return nw, ew
		}
		if nr != nw {
			return nw, io.ErrShortWrite
		}
	}
	return nr, er
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
