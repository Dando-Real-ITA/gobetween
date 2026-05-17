package cmd

/**
 * from-file.go - pull config from file and run
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
 * Add Root Command
 */
func init() {
	RootCmd.AddCommand(FromFileCmd)
}

/**
 * FromFile Command
 */
var FromFileCmd = &cobra.Command{
	Use:   "from-file <path>",
	Short: "Start using config from file",
	Run: func(cmd *cobra.Command, args []string) {

		if len(args) != 1 {
			cmd.Help()
			return
		}

		setConfigLoader(func() (*config.Config, error) {
			return loadConfigFromFile(args[0])
		})

		cfg, err := LoadConfig()
		if err != nil {
			log.Fatal(err)
		}

		info.Configuration = struct {
			Kind string `json:"kind"`
			Path string `json:"path"`
		}{"file", args[0]}

		start(cfg)
	},
}
