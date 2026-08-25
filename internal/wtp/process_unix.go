//go:build darwin || linux

package wtp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func launch(target Target, logPath string) (PreviewState, error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return PreviewState{}, fmt.Errorf("create log directory: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return PreviewState{}, fmt.Errorf("open preview log: %w", err)
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		_ = logFile.Close()
		return PreviewState{}, fmt.Errorf("open null input: %w", err)
	}

	command := exec.Command(target.Shell, "-lc", target.StartCommand)
	command.Dir = target.TargetApp
	command.Env = mergedEnvironment(map[string]string{
		"WORKTREE_PREVIEW_PORT":   strconv.Itoa(target.Port),
		"PORT":                    strconv.Itoa(target.Port),
		"WORKTREE_PREVIEW_BRANCH": target.Branch,
		"BROWSER":                 "none",
	})
	command.Stdin = devNull
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		_ = devNull.Close()
		_ = logFile.Close()
		return PreviewState{}, fmt.Errorf("start preview command: %w", err)
	}
	pid := command.Process.Pid
	_ = devNull.Close()
	_ = logFile.Close()

	fingerprint := ""
	for attempt := 0; attempt < 20 && fingerprint == ""; attempt++ {
		fingerprint, _ = processStartFingerprint(pid)
		if fingerprint == "" {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if fingerprint == "" {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		_ = command.Process.Release()
		return PreviewState{}, fmt.Errorf("could not identify started preview process")
	}
	if err := command.Process.Release(); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		return PreviewState{}, fmt.Errorf("detach preview process: %w", err)
	}

	return PreviewState{
		Version:        1,
		Status:         "starting",
		Port:           target.Port,
		Repository:     target.Repository,
		CommonDir:      target.CommonDir,
		TargetWorktree: target.TargetWorktree,
		TargetApp:      target.TargetApp,
		Branch:         target.Branch,
		Command:        target.StartCommand,
		PID:            pid,
		PGID:           pid,
		ProcessStarted: fingerprint,
		StartedAt:      time.Now().UTC(),
		LogPath:        logPath,
	}, nil
}

func waitUntilReady(state PreviewState, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processMatches(state) {
			return fmt.Errorf("preview process exited before port %d became ready", state.Port)
		}
		if portOpen(state.Port) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("preview did not become ready on port %d within %s", state.Port, timeout)
}

func stopProcess(state PreviewState) error {
	if !processMatches(state) {
		return fmt.Errorf("preview state no longer matches PID %d", state.PID)
	}
	if err := signalProcessGroup(state.PGID, syscall.SIGINT); err != nil {
		return fmt.Errorf("interrupt preview process group %d: %w", state.PGID, err)
	}
	if waitForProcessGroup(state.PGID, 5*time.Second) {
		return nil
	}
	if err := signalProcessGroup(state.PGID, syscall.SIGTERM); err != nil {
		return fmt.Errorf("terminate preview process group %d: %w", state.PGID, err)
	}
	if waitForProcessGroup(state.PGID, 3*time.Second) {
		return nil
	}
	return fmt.Errorf("preview process group %d did not stop", state.PGID)
}

func terminateStartedProcess(state PreviewState) error {
	if err := signalProcessGroup(state.PGID, syscall.SIGTERM); err != nil {
		return err
	}
	if waitForProcessGroup(state.PGID, 3*time.Second) {
		return nil
	}
	return fmt.Errorf("preview process group %d did not stop after failed launch", state.PGID)
}

func signalProcessGroup(pgid int, signal syscall.Signal) error {
	if pgid < 1 {
		return fmt.Errorf("invalid process group %d", pgid)
	}
	err := syscall.Kill(-pgid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func waitForProcessGroup(pgid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processGroupAlive(pgid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !processGroupAlive(pgid)
}

func processMatches(state PreviewState) bool {
	if !pidAlive(state.PID) || !processGroupAlive(state.PGID) {
		return false
	}
	pgid, err := syscall.Getpgid(state.PID)
	if err != nil || pgid != state.PGID {
		return false
	}
	fingerprint, err := processStartFingerprint(state.PID)
	return err == nil && fingerprint != "" && fingerprint == state.ProcessStarted
}

func processGroupAlive(pgid int) bool {
	if pgid < 1 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func processStartFingerprint(pid int) (string, error) {
	command := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func portOpen(port int) bool {
	addresses := []string{
		net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		net.JoinHostPort("::1", strconv.Itoa(port)),
	}
	for _, address := range addresses {
		connection, err := net.DialTimeout("tcp", address, 150*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return true
		}
	}
	return false
}

func runAttached(shell, directory, command string, out, errOut io.Writer) error {
	process := exec.Command(shell, "-lc", command)
	process.Dir = directory
	process.Stdin = os.Stdin
	process.Stdout = out
	process.Stderr = errOut
	return process.Run()
}

func mergedEnvironment(overrides map[string]string) []string {
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; !replaced {
			environment = append(environment, entry)
		}
	}
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	return environment
}

func writeLogTail(writer io.Writer, path string, limit int) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open preview log %s: %w", path, err)
	}
	defer file.Close()

	lines := make([]string, 0, limit)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if len(lines) == limit {
			copy(lines, lines[1:])
			lines[len(lines)-1] = scanner.Text()
		} else {
			lines = append(lines, scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read preview log: %w", err)
	}
	for _, line := range lines {
		fmt.Fprintln(writer, line)
	}
	return nil
}
