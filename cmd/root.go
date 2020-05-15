package cmd

import (
	"net/http"
	_ "net/http/pprof"

	homedir "github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := homedir.Dir()
		if err != nil {
			log.Fatalln("error finding home directory:", err)
		}
		viper.AddConfigPath(home)
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
}

func launchProfiler(addr string) {
	log.Printf("pprof listening on '%s'", addr)
	log.Println(http.ListenAndServe(addr, nil))
}
