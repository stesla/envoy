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

type proxy struct {
	Name     string
	Password string
	Server   string
}

func ReadProxies() (out map[string]*proxy) {
	out = make(map[string]*proxy)
	for name, _ := range viper.GetStringMapString("proxies") {
		p := viper.GetStringMapString("proxies." + name)
		out[name] = &proxy{
			Name:     name,
			Password: p["password"],
			Server:   p["server"],
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
	proxyName := strings.ToLower(strings.TrimSpace(line))
	obj, found := proxies.Load(proxyName)
	proxy, _ := obj.(*proxy)

	var password string
	if found {
		line, err = r.ReadString('\n')
		if err != nil {
			return
		}
		password = strings.TrimSpace(line)
	}

	if !found || password != proxy.Password {
		fmt.Fprintln(client, "invalid proxy name or password")
		return
	}

	server, err := net.Dial("tcp", proxy.Server)
	if err != nil {
		fmt.Fprintln(client, "error connecting to server:", err)
		return
	}
	defer server.Close()

	cch := make(chan bool)
	go func() {
		io.Copy(server, r)
		close(cch)
	}()

	sch := make(chan bool)
	go func() {
		io.Copy(client, server)
		close(sch)
	}()

	select {
	case <-cch:
		return

	case <-sch:
		fmt.Fprintln(client, "connection closed by server")
		return
	}
}
