package wtp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installedSet builds a presence function for detectStartCommand.
func installedSet(names ...string) func(string) bool {
	present := map[string]bool{}
	for _, name := range names {
		present[name] = true
	}
	return func(name string) bool { return present[name] }
}

func TestDetectStartCommandPrefersExplicitScript(t *testing.T) {
	pkg := packageJSON{Scripts: map[string]string{
		"worktree-preview": "vite --port 4000",
		"dev":              "vite",
	}}

	command, err := detectStartCommand("pnpm", pkg, installedSet("vite"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if command != "pnpm run worktree-preview" {
		t.Fatalf("got %q", command)
	}
}

func TestDetectStartCommandVite(t *testing.T) {
	pkg := packageJSON{Scripts: map[string]string{"dev": "vite"}}

	command, err := detectStartCommand("pnpm", pkg, installedSet("vite"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `pnpm run dev --port "$WORKTREE_PREVIEW_PORT" --strictPort`
	if command != want {
		t.Fatalf("got %q, want %q", command, want)
	}
}

// The framework is hoisted and absent from package.json, but the package manager sees it.
func TestDetectStartCommandUsesProbeNotManifest(t *testing.T) {
	pkg := packageJSON{Scripts: map[string]string{"dev": "next dev"}}

	command, err := detectStartCommand("npm", pkg, installedSet("next"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `npm run dev -- --port "$WORKTREE_PREVIEW_PORT"`
	if command != want {
		t.Fatalf("got %q, want %q", command, want)
	}
}

// A dev script that names a framework which is not installed must not be trusted.
func TestDetectStartCommandRejectsUninstalledFramework(t *testing.T) {
	pkg := packageJSON{Scripts: map[string]string{"dev": "vite"}}

	if _, err := detectStartCommand("pnpm", pkg, installedSet()); err == nil {
		t.Fatal("expected detection to fail when vite is not installed")
	}
}

// A dev server that opens a browser would hijack the user's session.
func TestDetectStartCommandRejectsAutoOpen(t *testing.T) {
	pkg := packageJSON{Scripts: map[string]string{"dev": "vite --open"}}

	if _, err := detectStartCommand("pnpm", pkg, installedSet("vite")); err == nil {
		t.Fatal("expected detection to reject a script that opens a browser")
	}
}

func TestDetectStartCommandNeedsPackageManager(t *testing.T) {
	pkg := packageJSON{Scripts: map[string]string{"dev": "vite"}}

	_, err := detectStartCommand("", pkg, installedSet("vite"))
	if err == nil {
		t.Fatal("expected detection to fail without a package manager")
	}
	if !strings.Contains(err.Error(), "worktreePreview.start") {
		t.Errorf("error should point at the config key, got: %v", err)
	}
}

// A configured start command is used as is, and never triggers detection.
func TestResolveStartCommandKeepsConfiguredCommand(t *testing.T) {
	target := Target{StartCommand: "make serve"}

	if err := target.ResolveStartCommand(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.StartCommand != "make serve" {
		t.Fatalf("got %q", target.StartCommand)
	}
}

func TestPackageScriptCommand(t *testing.T) {
	cases := []struct {
		manager string
		args    string
		want    string
	}{
		{"pnpm", "--port 3000", "pnpm run dev --port 3000"},
		{"npm", "--port 3000", "npm run dev -- --port 3000"},
		{"npm", "", "npm run dev"},
		{"yarn", "--port 3000", "yarn dev --port 3000"},
		{"bun", "--port 3000", "bun run dev --port 3000"},
	}
	for _, testCase := range cases {
		got := packageScriptCommand(testCase.manager, "dev", testCase.args)
		if got != testCase.want {
			t.Errorf("%s: got %q, want %q", testCase.manager, got, testCase.want)
		}
	}
}

func TestDetectPackageManagerPrefersDeclaredField(t *testing.T) {
	directory := t.TempDir()
	writeEmptyFile(t, filepath.Join(directory, "package-lock.json"))

	manager := detectPackageManager(directory, packageJSON{PackageManager: "pnpm@9.1.0"})
	if manager != "pnpm" {
		t.Fatalf("got %q, want pnpm", manager)
	}
}

func TestDetectPackageManagerFromLockfile(t *testing.T) {
	cases := map[string]string{
		"pnpm-lock.yaml":    "pnpm",
		"package-lock.json": "npm",
		"yarn.lock":         "yarn",
		"bun.lockb":         "bun",
	}
	for lockfile, want := range cases {
		directory := t.TempDir()
		writeEmptyFile(t, filepath.Join(directory, lockfile))
		if got := detectPackageManager(directory, packageJSON{}); got != want {
			t.Errorf("%s: got %q, want %q", lockfile, got, want)
		}
	}
}

func TestDetectPackageManagerReturnsEmptyWhenUnknown(t *testing.T) {
	if manager := detectPackageManager(t.TempDir(), packageJSON{}); manager != "" {
		t.Fatalf("got %q, want an empty manager", manager)
	}
}

func TestSafeRelativePathRejectsEscapes(t *testing.T) {
	for _, value := range []string{"", "/etc", "../outside", ".."} {
		if _, err := safeRelativePath(value); err == nil {
			t.Errorf("expected %q to be rejected", value)
		}
	}
	clean, err := safeRelativePath("apps/web/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clean != filepath.Join("apps", "web") {
		t.Fatalf("got %q", clean)
	}
}

func writeEmptyFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}
