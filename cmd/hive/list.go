package main

import (
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:        "list",
	Short:      "Show all sessions",
	Long:       "Lists all sessions. Deprecated: use 'hive ls' instead.",
	Args:       cobra.NoArgs,
	Deprecated: "use 'hive ls' instead",
	RunE:       runLs,
}

func init() {
	rootCmd.AddCommand(listCmd)
}
