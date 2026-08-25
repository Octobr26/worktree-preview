package wtp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type PreviewState struct {
	Version        int       `json:"version"`
	Status         string    `json:"status"`
	Port           int       `json:"port"`
	Repository     string    `json:"repository"`
	CommonDir      string    `json:"commonDir"`
	TargetWorktree string    `json:"targetWorktree"`
	TargetApp      string    `json:"targetApp"`
	Branch         string    `json:"branch"`
	Command        string    `json:"command"`
	PID            int       `json:"pid"`
	PGID           int       `json:"pgid"`
	ProcessStarted string    `json:"processStarted"`
	StartedAt      time.Time `json:"startedAt"`
	LogPath        string    `json:"logPath"`
}

type stateStore struct {
	dir string
}

func (s *stateStore) stateDir() string {
	return filepath.Join(s.dir, "previews")
}

func (s *stateStore) statePath(port int) string {
	return filepath.Join(s.stateDir(), strconv.Itoa(port)+".json")
}

func (s *stateStore) logPath(port int) string {
	return filepath.Join(s.dir, "logs", strconv.Itoa(port)+".log")
}

func (s *stateStore) lockPath(port int) string {
	return filepath.Join(s.dir, "locks", strconv.Itoa(port)+".lock")
}

func (s *stateStore) load(port int) (*PreviewState, error) {
	data, err := os.ReadFile(s.statePath(port))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read preview state for port %d: %w", port, err)
	}
	var state PreviewState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse preview state for port %d: %w", port, err)
	}
	if state.Version != 1 || state.Port != port || state.PID < 1 || state.PGID < 1 {
		return nil, fmt.Errorf("invalid preview state for port %d", port)
	}
	return &state, nil
}

func (s *stateStore) write(state PreviewState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preview state: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(s.stateDir(), 0o700); err != nil {
		return fmt.Errorf("create preview state directory: %w", err)
	}
	if err := writeFileAtomic(s.statePath(state.Port), data, 0o600); err != nil {
		return fmt.Errorf("write preview state: %w", err)
	}
	return nil
}

func (s *stateStore) remove(port int) error {
	err := os.Remove(s.statePath(port))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove preview state for port %d: %w", port, err)
	}
	return nil
}

func (s *stateStore) list() ([]PreviewState, error) {
	entries, err := os.ReadDir(s.stateDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list preview state: %w", err)
	}
	states := make([]PreviewState, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		port, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			continue
		}
		state, err := s.load(port)
		if err != nil {
			return nil, err
		}
		if state != nil {
			states = append(states, *state)
		}
	}
	return states, nil
}

func (s *stateStore) acquire(port int) (func(), error) {
	lockPath := s.lockPath(port)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			if _, writeErr := fmt.Fprintf(file, "%d\n", os.Getpid()); writeErr != nil {
				_ = file.Close()
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("write operation lock: %w", writeErr)
			}
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("close operation lock: %w", closeErr)
			}
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("create operation lock: %w", err)
		}
		ownerData, readErr := os.ReadFile(lockPath)
		owner, parseErr := strconv.Atoi(strings.TrimSpace(string(ownerData)))
		if readErr == nil && parseErr == nil && pidAlive(owner) {
			return nil, fmt.Errorf("another worktree-preview operation is already using port %d", port)
		}
		if removeErr := os.Remove(lockPath); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			return nil, fmt.Errorf("remove stale operation lock: %w", removeErr)
		}
	}
	return nil, fmt.Errorf("could not acquire operation lock for port %d", port)
}

func stateHealth(state PreviewState) string {
	if !processMatches(state) {
		return "stale"
	}
	if !portOpen(state.Port) {
		return "starting"
	}
	return "running"
}

func pidAlive(pid int) bool {
	if pid < 1 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func writeFileAtomic(path string, data []byte, mode fs.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
