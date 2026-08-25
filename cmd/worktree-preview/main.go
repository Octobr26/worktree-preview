package main

import (
	"fmt"
	"os"

	"github.com/Octobr26/worktree-preview/internal/wtp"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "worktree-preview: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	app, err := wtp.NewApp(os.Stdout, os.Stderr)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		app.Usage()
		return nil
	}

	switch args[0] {
	case "use":
		if len(args) != 2 {
			return fmt.Errorf("usage: worktree-preview use <branch>")
		}
		return app.Use(args[1])
	case "stop":
		if len(args) != 1 {
			return fmt.Errorf("usage: worktree-preview stop")
		}
		return app.Stop()
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("usage: worktree-preview status")
		}
		return app.Status()
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: worktree-preview list")
		}
		return app.List()
	case "logs":
		if len(args) != 1 {
			return fmt.Errorf("usage: worktree-preview logs")
		}
		return app.Logs()
	case "dry-run", "--dry-run":
		if len(args) != 2 {
			return fmt.Errorf("usage: worktree-preview dry-run <branch>")
		}
		return app.DryRun(args[1])
	case "help", "-h", "--help":
		app.Usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
