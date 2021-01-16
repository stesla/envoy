package cmd

import (
	"envoy/proxy"
	"envoy/telnet"
	"net"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	proxyCmd = &cobra.Command{
		Use:   "proxy",
		Short: "start proxy server",
		Run:   startProxy,
	}
)

func init() {
	proxyCmd.PersistentFlags().StringP("listen", "l", ":4001", "address to listen on")
	viper.BindPFlag("listen", proxyCmd.PersistentFlags().Lookup("listen"))
	rootCmd.AddCommand(proxyCmd)
}

func startProxy(cmd *cobra.Command, args []string) {
	addr := viper.GetString("listen")
	log.Printf("envoy (pid %d) listening on '%s'", os.Getpid(), addr)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()

	signal.Ignore(os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)

	ch1 := make(chan os.Signal)
	signal.Notify(ch1, syscall.SIGHUP)
	go func() {
		for _ = range ch1 {
			proxy.ReopenLogs()
		}
	}()

	ch2 := make(chan os.Signal)
	signal.Notify(ch2, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-ch2
		log.Infof("received signal '%s', exiting", sig)
		proxy.CloseAll()
		os.Exit(0)
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}

		logEntry := log.WithFields(log.Fields{
			"type": telnet.ClientType,
			"addr": conn.RemoteAddr(),
		})
		client := telnet.Wrap(telnet.ClientType, conn)
		client.SetLog(logEntry)
		client.GetOption(telnet.Charset).Allow(true, true)
		client.GetOption(telnet.EndOfRecord).Allow(true, true)
		client.GetOption(telnet.SuppressGoAhead).Allow(true, true)
		client.GetOption(telnet.TransmitBinary).Allow(true, true)
		client.GetOption(telnet.EndOfRecord).EnableThem()
		client.GetOption(telnet.EndOfRecord).EnableUs()
		client.GetOption(telnet.SuppressGoAhead).EnableThem()
		client.GetOption(telnet.SuppressGoAhead).EnableUs()

		client.RegisterHandler(&telnet.CharsetOption{})
		client.GetOption(telnet.Charset).EnableThem()
		client.GetOption(telnet.Charset).EnableUs()

		go proxy.StartSession(client, logEntry)
	}

}
