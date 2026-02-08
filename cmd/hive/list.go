package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/ivy/hive/internal/workspace"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Show all workspaces",
	Long:  "Lists all hive workspaces with their status, repo, issue, and path.",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	workspaces, err := workspace.ListAll(cmd.Context())
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}

	if len(workspaces) == 0 {
		fmt.Println("No workspaces found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tREPO\tISSUE\tBRANCH\tPATH")
	for _, ws := range workspaces {
		fmt.Fprintf(w, "%s\t%s\t#%d\t%s\t%s\n",
			ws.Status, ws.Repo, ws.IssueNumber, ws.Branch, ws.Path)
	}
	return w.Flush()
}
