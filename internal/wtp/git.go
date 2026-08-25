package wtp

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type worktree struct {
	Path   string
	Branch string
}

func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func gitOutputAt(root string, args ...string) (string, error) {
	fullArgs := append([]string{"-C", root}, args...)
	return gitOutput(fullArgs...)
}

func configValue(root, key string) string {
	value, err := gitOutputAt(root, "config", "--local", "--get", key)
	if err != nil {
		return ""
	}
	return value
}

func configValues(root, key string) []string {
	cmd := exec.Command("git", "-C", root, "config", "--local", "--get-all", key)
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	values := make([]string, 0, len(lines))
	for _, line := range lines {
		if value := strings.TrimSpace(line); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func resolveWorktrees(root, wantedBranch string) (string, string, error) {
	cmd := exec.Command("git", "-C", root, "worktree", "list", "--porcelain", "-z")
	output, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("list worktrees: %w", err)
	}

	var worktrees []worktree
	var current *worktree
	for _, field := range bytes.Split(output, []byte{0}) {
		value := string(field)
		switch {
		case strings.HasPrefix(value, "worktree "):
			worktrees = append(worktrees, worktree{Path: strings.TrimPrefix(value, "worktree ")})
			current = &worktrees[len(worktrees)-1]
		case current != nil && strings.HasPrefix(value, "branch refs/heads/"):
			current.Branch = strings.TrimPrefix(value, "branch refs/heads/")
		}
	}
	if len(worktrees) == 0 {
		return "", "", fmt.Errorf("repository has no worktrees")
	}
	mainWorktree, err := canonicalPath(worktrees[0].Path)
	if err != nil {
		return "", "", fmt.Errorf("main worktree is unavailable: %w", err)
	}
	for _, candidate := range worktrees {
		if candidate.Branch != wantedBranch {
			continue
		}
		target, err := canonicalPath(candidate.Path)
		if err != nil {
			return "", "", fmt.Errorf("target worktree is unavailable: %w", err)
		}
		return mainWorktree, target, nil
	}
	return "", "", fmt.Errorf("no existing worktree for branch: %s", wantedBranch)
}

func (t Target) ensureEnvironmentFiles(out io.Writer) error {
	for _, configured := range t.EnvFiles {
		envFile, err := safeRelativePath(configured)
		if err != nil {
			return fmt.Errorf("unsafe env-file path %q: %w", configured, err)
		}
		source := filepath.Join(t.MainApp, envFile)
		target := filepath.Join(t.TargetApp, envFile)
		if _, err := os.Lstat(source); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect environment source %s: %w", source, err)
		}
		if info, err := os.Lstat(target); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				if _, err := os.Stat(target); err != nil {
					return fmt.Errorf("dangling environment symlink: %s", target)
				}
			}
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect environment target %s: %w", target, err)
		}

		repositoryRelative := envFile
		if t.AppDir != "." {
			repositoryRelative = filepath.Join(t.AppDir, envFile)
		}
		check := exec.Command("git", "-C", t.TargetWorktree, "check-ignore", "-q", "--", repositoryRelative)
		if err := check.Run(); err != nil {
			return fmt.Errorf("refusing to link environment file that Git does not ignore: %s", repositoryRelative)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return fmt.Errorf("create environment directory: %w", err)
		}
		if err := os.Symlink(source, target); err != nil {
			return fmt.Errorf("link %s: %w", target, err)
		}
		fmt.Fprintf(out, "Linked %s\n", envFile)
	}
	return nil
}
