package wtp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var dependencyFiles = []string{
	"package.json",
	"pnpm-lock.yaml",
	"pnpm-workspace.yaml",
	"package-lock.json",
	"npm-shrinkwrap.json",
	"yarn.lock",
	"bun.lock",
	"bun.lockb",
}

func (t Target) ensureDependencies(stateDir string, out, errOut io.Writer) error {
	if t.InstallCommand == "" {
		return nil
	}
	expected, err := t.dependencyHash()
	if err != nil {
		return err
	}
	stamp := filepath.Join(stateDir, "dependencies", shortHash(t.CommonDir)+"-"+shortHash(t.TargetApp)+".sha256")
	installed, readErr := os.ReadFile(stamp)
	dependenciesPresent := t.dependenciesPresent()
	if dependenciesPresent && readErr == nil && string(installed) == expected+"\n" {
		return nil
	}

	switch {
	case !dependenciesPresent:
		fmt.Fprintf(out, "Installing dependencies in %s\n", t.TargetApp)
	case os.IsNotExist(readErr):
		fmt.Fprintf(out, "Verifying dependencies in %s\n", t.TargetApp)
	default:
		fmt.Fprintln(out, "Dependency files changed; refreshing dependencies")
	}
	if err := runAttached(t.Shell, t.TargetApp, t.InstallCommand, out, errOut); err != nil {
		return fmt.Errorf("dependency install failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(stamp), 0o700); err != nil {
		return fmt.Errorf("create dependency state directory: %w", err)
	}
	if err := writeFileAtomic(stamp, []byte(expected+"\n"), 0o600); err != nil {
		return fmt.Errorf("write dependency stamp: %w", err)
	}
	return nil
}

func (t Target) dependencyHash() (string, error) {
	hash := sha256.New()
	fmt.Fprintf(hash, "install %s\n", t.InstallCommand)
	found := false
	for _, name := range dependencyFiles {
		path := filepath.Join(t.TargetApp, name)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("hash %s: %w", path, err)
		}
		found = true
		fileHash := sha256.Sum256(data)
		fmt.Fprintf(hash, "%x %s\n", fileHash, name)
	}
	if !found {
		return "", fmt.Errorf("could not hash dependency files")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (t Target) dependenciesPresent() bool {
	if t.PackageManager == "pnpm" {
		_, err := os.Stat(filepath.Join(t.TargetApp, "node_modules", ".modules.yaml"))
		return err == nil
	}
	for _, path := range []string{
		filepath.Join(t.TargetApp, "node_modules"),
		filepath.Join(t.TargetApp, ".pnp.cjs"),
		filepath.Join(t.TargetApp, ".pnp.js"),
	} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}
