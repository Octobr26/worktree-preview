package wtp

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

const readinessTimeout = 15 * time.Second

type App struct {
	out   io.Writer
	err   io.Writer
	store *stateStore
}

func NewApp(out, errOut io.Writer) (*App, error) {
	stateDir, err := defaultStateDir()
	if err != nil {
		return nil, err
	}
	return &App{out: out, err: errOut, store: &stateStore{dir: stateDir}}, nil
}

func (a *App) Usage() {
	fmt.Fprintln(a.out, `Usage:
  worktree-preview use <branch>
  worktree-preview stop
  worktree-preview status
  worktree-preview list
  worktree-preview logs
  worktree-preview dry-run <branch>

A repository previews one worktree at a time on its configured port.
Running use against another worktree switches that port over to it.

Short alias after installation: wtp

Optional per-repository Git config:
  worktreePreview.appDir
  worktreePreview.port
  worktreePreview.packageManager
  worktreePreview.install
  worktreePreview.start
  worktreePreview.envFile (repeatable)
  worktreePreview.shell`)
}

func (a *App) Use(branch string) error {
	repo, err := loadRepository()
	if err != nil {
		return err
	}
	target, err := repo.prepareTarget(strings.TrimPrefix(branch, "refs/heads/"))
	if err != nil {
		return err
	}
	if target.PackageManager != "" {
		if _, err := exec.LookPath(target.PackageManager); err != nil {
			return fmt.Errorf("%s not found", target.PackageManager)
		}
	}

	release, err := a.store.acquire(target.Port)
	if err != nil {
		return err
	}
	defer release()

	existing, err := a.store.load(target.Port)
	if err != nil {
		return err
	}
	if existing != nil {
		decision, err := classifyExisting(*existing, target, processMatches(*existing), portOpen(target.Port))
		if err != nil {
			return err
		}
		switch decision {
		case decisionAlreadyServing:
			if existing.Status != "running" {
				existing.Status = "running"
				if err := a.store.write(*existing); err != nil {
					return err
				}
			}
			fmt.Fprintf(a.out, "Already serving %s\n", target.Label)
			fmt.Fprintf(a.out, "url: http://localhost:%d\n", target.Port)
			return nil
		case decisionClearStale:
			if err := a.store.remove(target.Port); err != nil {
				return err
			}
			existing = nil
		case decisionRestart, decisionSwitch:
			// The running preview is stopped and replaced further below, once the
			// new target's dependencies and start command have been resolved.
		}
	}

	if err := target.ensureEnvironmentFiles(a.out); err != nil {
		return err
	}
	if err := target.ensureDependencies(a.store.dir, a.out, a.err); err != nil {
		return err
	}
	if err := target.ResolveStartCommand(); err != nil {
		return err
	}

	if existing != nil {
		fmt.Fprintf(a.out, "Stopping %s on port %d\n", existing.Branch, target.Port)
		if err := stopProcess(*existing); err != nil {
			return err
		}
		if err := a.store.remove(target.Port); err != nil {
			return err
		}
	}
	if portOpen(target.Port) {
		return fmt.Errorf("port %d already has an unowned listener", target.Port)
	}

	state, err := launch(target, a.store.logPath(target.Port))
	if err != nil {
		return err
	}
	if err := a.store.write(state); err != nil {
		return a.cleanupFailedLaunch(state, err)
	}

	if err := waitUntilReady(state, readinessTimeout); err != nil {
		result := a.cleanupFailedLaunch(state, err)
		fmt.Fprintf(a.err, "\nLast preview log lines:\n")
		_ = writeLogTail(a.err, state.LogPath, 40)
		return result
	}

	state.Status = "running"
	if err := a.store.write(state); err != nil {
		return a.cleanupFailedLaunch(state, err)
	}

	fmt.Fprintf(a.out, "Serving %s\n", target.Label)
	fmt.Fprintf(a.out, "url: http://localhost:%d\n", target.Port)
	fmt.Fprintf(a.out, "logs: %s\n", state.LogPath)
	return nil
}

func (a *App) cleanupFailedLaunch(state PreviewState, cause error) error {
	if err := terminateStartedProcess(state); err != nil {
		return fmt.Errorf("%v; failed-launch cleanup also failed: %w", cause, err)
	}
	if err := a.store.remove(state.Port); err != nil {
		return fmt.Errorf("%v; preview stopped but state cleanup failed: %w", cause, err)
	}
	return cause
}

func (a *App) Stop() error {
	repo, err := loadRepository()
	if err != nil {
		return err
	}
	port := repo.Config.Port
	release, err := a.store.acquire(port)
	if err != nil {
		return err
	}
	defer release()

	state, err := a.store.load(port)
	if err != nil {
		return err
	}
	if state == nil {
		fmt.Fprintf(a.out, "No managed preview on port %d\n", port)
		return nil
	}
	if state.CommonDir != repo.CommonDir {
		return fmt.Errorf("port %d is managed by another repository: %s", state.Port, state.Repository)
	}
	if !processMatches(*state) {
		if processGroupAlive(state.PGID) {
			return fmt.Errorf("preview state no longer matches PID %d; refusing to signal an unverified process group", state.PID)
		}
		if err := a.store.remove(state.Port); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "Cleared stale preview state for port %d\n", state.Port)
		return nil
	}
	if err := stopProcess(*state); err != nil {
		return err
	}
	if err := a.store.remove(state.Port); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Stopped preview on port %d (%s in %s)\n", state.Port, state.Branch, state.TargetWorktree)
	return nil
}

func (a *App) Status() error {
	repo, err := loadRepository()
	if err != nil {
		return err
	}
	port := repo.Config.Port
	state, err := a.store.load(port)
	if err != nil {
		return err
	}
	if state == nil {
		fmt.Fprintf(a.out, "preview: none\nport: %d\nstate: none\n", port)
		return nil
	}
	if state.CommonDir != repo.CommonDir {
		return fmt.Errorf("port %d is managed by another repository: %s", state.Port, state.Repository)
	}
	health := stateHealth(*state)
	fmt.Fprintf(a.out, "preview: %d -> %s\n", state.Port, filepath.Base(state.TargetWorktree))
	fmt.Fprintf(a.out, "repository: %s\n", state.Repository)
	fmt.Fprintf(a.out, "branch: %s\n", state.Branch)
	fmt.Fprintf(a.out, "port: %d\n", state.Port)
	fmt.Fprintf(a.out, "pid: %d\n", state.PID)
	fmt.Fprintf(a.out, "cwd: %s\n", state.TargetApp)
	fmt.Fprintf(a.out, "logs: %s\n", state.LogPath)
	fmt.Fprintf(a.out, "state: %s\n", health)
	return nil
}

func (a *App) List() error {
	states, err := a.store.list()
	if err != nil {
		return err
	}
	if len(states) == 0 {
		fmt.Fprintln(a.out, "No managed previews")
		return nil
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Port < states[j].Port })
	tw := tabwriter.NewWriter(a.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PORT\tSTATE\tREPOSITORY\tBRANCH\tWORKTREE")
	for _, state := range states {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", state.Port, stateHealth(state), state.Repository, state.Branch, state.TargetWorktree)
	}
	return tw.Flush()
}

func (a *App) Logs() error {
	repo, err := loadRepository()
	if err != nil {
		return err
	}
	port := repo.Config.Port
	state, err := a.store.load(port)
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("no managed preview on port %d", port)
	}
	if state.CommonDir != repo.CommonDir {
		return fmt.Errorf("port %d is managed by another repository: %s", state.Port, state.Repository)
	}
	return writeLogTail(a.out, state.LogPath, 80)
}

func (a *App) DryRun(branch string) error {
	repo, err := loadRepository()
	if err != nil {
		return err
	}
	target, err := repo.prepareTarget(strings.TrimPrefix(branch, "refs/heads/"))
	if err != nil {
		return err
	}
	startError := target.ResolveStartCommand()
	fmt.Fprintf(a.out, "branch: %s\n", target.Branch)
	fmt.Fprintf(a.out, "worktree: %s\n", target.TargetWorktree)
	fmt.Fprintf(a.out, "app: %s\n", target.TargetApp)
	fmt.Fprintf(a.out, "port: %d\n", target.Port)
	manager := target.PackageManager
	if manager == "" {
		manager = "custom"
	}
	fmt.Fprintf(a.out, "package manager: %s\n", manager)
	install := target.InstallCommand
	if install == "" {
		install = "none"
	}
	fmt.Fprintf(a.out, "install: %s\n", install)
	if startError != nil {
		// Detection queries the package manager, so it can only succeed once
		// dependencies are installed. A dry run must still report the rest.
		fmt.Fprintf(a.out, "start: unresolved (%v)\n", startError)
	} else {
		fmt.Fprintf(a.out, "start: %s\n", target.StartCommand)
	}
	if len(target.EnvFiles) == 0 {
		fmt.Fprintln(a.out, "environment files: none")
	} else {
		fmt.Fprintf(a.out, "environment files: %s\n", strings.Join(target.EnvFiles, ", "))
	}
	return nil
}

func defaultStateDir() (string, error) {
	if configured := os.Getenv("XDG_STATE_HOME"); configured != "" {
		return filepath.Join(configured, "worktree-preview"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "worktree-preview"), nil
}
