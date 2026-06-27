package cli

import (
	"fmt"
	"os"

	"github.com/scottymacleod/aegis/internal/worktree"
	"github.com/spf13/cobra"
)

func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "Manage git worktrees for isolated parallel sessions",
		Long: "Create and manage git worktrees so a session can run in an isolated working tree. " +
			"Run `aegis` from inside a worktree directory to get a session scoped to it — the standard " +
			"mechanism for safe parallel agent work. Worktrees live under .aegis/worktrees/ in the repo.",
	}
	cmd.AddCommand(newWorktreeAddCmd(), newWorktreeListCmd(), newWorktreeRemoveCmd(), newWorktreePruneCmd())
	return cmd
}

func worktreeManager() (*worktree.Manager, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return worktree.NewManager(cwd)
}

func newWorktreeAddCmd() *cobra.Command {
	var branch string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a worktree (and, with --branch, a new branch)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := worktreeManager()
			if err != nil {
				return err
			}
			path, err := m.Add(args[0], branch)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created worktree %s\n  cd %s && aegis\n", args[0], path)
			return nil
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "create and check out a new branch of this name")
	return cmd
}

func newWorktreeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the repository's worktrees",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := worktreeManager()
			if err != nil {
				return err
			}
			list, err := m.List()
			if err != nil {
				return err
			}
			for _, w := range list {
				branch := w.Branch
				if branch == "" {
					branch = "(detached)"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%-40s %s\n", w.Path, branch)
			}
			return nil
		},
	}
}

func newWorktreeRemoveCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a managed worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := worktreeManager()
			if err != nil {
				return err
			}
			if err := m.Remove(args[0], force); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed worktree %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "discard uncommitted changes in the worktree")
	return cmd
}

func newWorktreePruneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prune",
		Short: "Prune administrative files for deleted worktrees",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := worktreeManager()
			if err != nil {
				return err
			}
			if err := m.Prune(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "pruned")
			return nil
		},
	}
}
