package cli

import (
	"github.com/spf13/cobra"
	"github.com/user/agentloop/internal/config"
	"github.com/user/agentloop/internal/logging"
)

var (
	cfgFile    string
	cfg        *config.Config
	appVersion string
)

var rootCmd = &cobra.Command{
	Use:   "agentloop",
	Short: "AI agent orchestrator built on pi (shittycodingagent.ai)",
}

func Execute(version string) error {
	appVersion = version
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path")
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(sessionCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use: "version", Short: "Print version info",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Printf("agentloop %s (powered by pi)\n", appVersion)
	},
}

func initConfig() {
	path := cfgFile
	if path == "" {
		path = config.DefaultConfigPath()
	}
	loaded, err := config.Load(path)
	if err != nil {
		loaded = config.Defaults()
	}
	cfg = loaded
	_ = logging.Init(cfg.Logging.Level, cfg.Logging.File)
}
