package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use: "session", Short: "Manage sessions",
}

var sessionListCmd = &cobra.Command{
	Use: "list", Short: "List saved sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO: list vault sessions
		fmt.Println("Session listing — coming in Phase 2")
		return nil
	},
}

func init() { sessionCmd.AddCommand(sessionListCmd) }
