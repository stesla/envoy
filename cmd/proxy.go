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

func start(cmd *cobra.Command, args []string) {
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

		fields := log.Fields{"type": telnet.ClientType, "addr": conn.RemoteAddr()}
		go proxy.StartSession(telnet.Wrap(fields, conn))
	}

}
