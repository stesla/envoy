package cmd

import (
	"net/http"
	_ "net/http/pprof"
	"os/user"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/text/encoding/ianaindex"
)

var (
	cfgFile string

	rootCmd = &cobra.Command{
		Use:   "envoy",
		Short: "envoy is a simple password-protected TCP proxy",
	}
)

func Execute() {
	rootCmd.Execute()
}

func init() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.envoyrc)")
}

func initConfig() {
	var err error
	u, err := user.Current()
	if err != nil {
		log.Fatalln(err)
	}
	viper.Set("user.home", u.HomeDir)

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath(u.HomeDir)
		viper.SetConfigName(".envoy.yaml")
		viper.SetConfigType("yaml")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Fatalln(err)
	} else {
		log.Printf("loaded config at %q", viper.ConfigFileUsed())
	}

	viper.WatchConfig()

	if addr := viper.GetString("debugaddr"); addr != "" {
		go launchProfiler(addr)
	}

	if str := viper.GetString("loglevel"); str != "" {
		level, err := log.ParseLevel(str)
		if err != nil {
			log.Fatalln("error parsing loglevel:", err)
		}
		log.SetLevel(level)
		log.Println("loglevel set to", level)
	}

	for _, key := range viper.AllKeys() {
		if strings.HasSuffix(key, ".encoding") {
			encName := viper.GetString(key)
			_, err := ianaindex.IANA.Encoding(encName)
			if err != nil {
				log.Fatalf("error loading encoding %q: %v", encName, err)
			}
		}
	}
}

func launchProfiler(addr string) {
	log.Printf("pprof listening on '%s'", addr)
	log.Println(http.ListenAndServe(addr, nil))
}
