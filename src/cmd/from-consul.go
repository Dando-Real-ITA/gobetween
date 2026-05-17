package cmd

/**
 * from-consul.go - pull config from consul and run
 *
 * @author Yaroslav Pogrebnyak <yyyaroslav@gmail.com>
 */

import (
	"log"

	consul "github.com/hashicorp/consul/api"
	"github.com/spf13/cobra"
	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/info"
)

/* Parsed options */
var consulKey string
var consulConfig consul.Config = consul.Config{}

/**
 * Add command
 */
func init() {

	FromConsulCmd.Flags().StringVarP(&consulKey, "key", "k", "gobetween", "Consul Key to pull config from")
	FromConsulCmd.Flags().StringVarP(&consulConfig.Scheme, "scheme", "s", "http", "http or https")

	RootCmd.AddCommand(FromConsulCmd)
}

/**
 * FromConsul command
 */
var FromConsulCmd = &cobra.Command{
	Use:   "from-consul <host:port>",
	Short: "Start using config from Consul",
	Long:  `Start using config from the Consul key-value storage`,
	Run: func(cmd *cobra.Command, args []string) {

		if len(args) != 1 {
			cmd.Help()
			return
		}

		setConfigLoader(func() (*config.Config, error) {
			return loadConfigFromConsul(args[0], consulKey, consulConfig)
		})

		cfg, err := LoadConfig()
		if err != nil {
			log.Fatal(err)
		}

		info.Configuration = struct {
			Kind string `json:"kind"`
			Host string `json:"host"`
			Key  string `json:"key"`
		}{"consul", args[0], consulKey}

		start(cfg)
	},
}
