package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "hive",
	Short: "Agent orchestrator — turns GitHub issues into pull requests",
	Long: `Hive dispatches Claude Code agents in isolated workspaces.
It polls a GitHub Projects board for ready items, creates git worktrees,
runs agents inside sandboxed environments, and opens PRs with the results.`,
	SilenceUsage: true,
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().String("config", "", "config file (default: .hive.toml)")
	rootCmd.PersistentFlags().Bool("verbose", false, "enable debug logging")
}

func initConfig() {
	if cfgFile, _ := rootCmd.Flags().GetString("config"); cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName(".hive")
		viper.SetConfigType("toml")
		viper.AddConfigPath(".")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		// Config file is optional — not an error if missing.
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "warning: config file: %v\n", err)
		}
	}
}

func setupLogger() *slog.Logger {
	verbose, _ := rootCmd.Flags().GetBool("verbose")

	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func main() {
	setupLogger()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
