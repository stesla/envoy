package cmd

import (
	"log"
	"net/http"
	_ "net/http/pprof"

	homedir "github.com/mitchellh/go-homedir"
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

	if err := viper.ReadInConfig(); err == nil {
		log.Printf("loaded config at '%s'", viper.ConfigFileUsed())
	}

	viper.WatchConfig()

	if addr := viper.GetString("debugaddr"); addr != "" {
		go launchProfiler(addr)
	}
}

func launchProfiler(addr string) {
	log.Printf("pprof listening on '%s'", addr)
	log.Println(http.ListenAndServe(addr, nil))
}
