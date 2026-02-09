package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

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

	rootCmd.PersistentFlags().String("config", "", "config file (default: ~/.config/hive/config.toml)")
	rootCmd.PersistentFlags().Bool("verbose", false, "enable debug logging")
}

func initConfig() {
	if cfgFile, _ := rootCmd.Flags().GetString("config"); cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("toml")
		viper.AddConfigPath(".")

		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			viper.AddConfigPath(filepath.Join(xdg, "hive"))
		}
		if home, err := os.UserHomeDir(); err == nil {
			viper.AddConfigPath(filepath.Join(home, ".config", "hive"))
		}
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
