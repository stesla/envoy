package cmd

import (
	"log"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/client-go/util/homedir"
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
		home := homedir.HomeDir()
		viper.AddConfigPath(home)
		viper.SetConfigName(".envoy.yaml")
		viper.SetConfigType("yaml")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		log.Printf("loaded config at '%s'", viper.ConfigFileUsed())
	}
}
