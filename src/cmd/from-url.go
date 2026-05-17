package cmd

/**
 * from-url.go - pull config from url and run
 *
 * @author Yaroslav Pogrebnyak <yyyaroslav@gmail.com>
 */

import (
	"log"

	"github.com/spf13/cobra"
	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/info"
)

/**
 * Add command
 */
func init() {

	RootCmd.AddCommand(FromUrlCmd)
}

/**
 * FromUrlCmd command
 */
var FromUrlCmd = &cobra.Command{
	Use:   "from-url <url>",
	Short: "Start using config from URL",
	Run: func(cmd *cobra.Command, args []string) {

		if len(args) != 1 {
			cmd.Help()
			return
		}

		setConfigLoader(func() (*config.Config, error) {
			return loadConfigFromURL(args[0])
		})

		cfg, err := LoadConfig()
		if err != nil {
			log.Fatal(err)
		}

		info.Configuration = struct {
			Kind string `json:"kind"`
			Url  string `json:"url"`
		}{"url", args[0]}

		start(cfg)
	},
}
