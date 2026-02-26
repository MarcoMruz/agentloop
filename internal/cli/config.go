package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{Use: "config", Short: "Manage configuration"}

var configShowCmd = &cobra.Command{
	Use: "show", Short: "Print current config",
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := yaml.Marshal(cfg)
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	},
}

func init() { configCmd.AddCommand(configShowCmd) }
