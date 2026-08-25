package wtp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQueryOutputHasPackagePathOutput(t *testing.T) {
	output := "/repo/app\n/repo/app/node_modules/vite\n/repo/app/node_modules/vite/node_modules/esbuild\n"

	if !queryOutputHasPackage(output, "vite", true) {
		t.Error("expected vite to be found in path output")
	}
	if queryOutputHasPackage(output, "next", true) {
		t.Error("did not expect next to be found in path output")
	}
}

// npm prints the project root even when the package is absent.
// A root-only listing must not count as a match.
func TestQueryOutputHasPackageIgnoresRootOnlyListing(t *testing.T) {
	if queryOutputHasPackage("/repo/app\n", "vite", true) {
		t.Error("a root-only listing must not report the package as present")
	}
}

func TestQueryOutputHasPackageTreeOutput(t *testing.T) {
	output := "app@1.0.0 node_modules\n├── vite@5.4.0\n└── @vitejs/plugin-react@4.3.1\n"

	if !queryOutputHasPackage(output, "vite", false) {
		t.Error("expected vite to be found in tree output")
	}
	if queryOutputHasPackage(output, "vitest", false) {
		t.Error("did not expect vitest to be found in tree output")
	}
}

// A scoped package must not satisfy a query for its unscoped prefix.
func TestQueryOutputHasPackageIgnoresScopedPrefix(t *testing.T) {
	if queryOutputHasPackage("└── @vitejs/plugin-react@4.3.1\n", "vitejs", false) {
		t.Error("a scoped package must not match its unscoped prefix")
	}
}

func TestPackageQueryForUnsupportedManager(t *testing.T) {
	if _, supported := packageQueryFor("yarn"); supported {
		t.Error("yarn has no single query shape; it must fall back to the installed tree")
	}
	for _, manager := range []string{"pnpm", "npm", "bun"} {
		if _, supported := packageQueryFor(manager); !supported {
			t.Errorf("%s should have a query", manager)
		}
	}
}

// Hoisted monorepo installs put the package above the app directory.
func TestInstalledInTreeFindsHoistedPackage(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "node_modules", "vite", "package.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	probe := &packageProbe{app: app, root: root, cache: map[string]bool{}}
	if !probe.installedInTree("vite") {
		t.Error("expected the hoisted package to be found at the worktree root")
	}
	if probe.installedInTree("next") {
		t.Error("did not expect next to be found")
	}
}

// The probe must not escape the worktree while walking upward.
func TestInstalledInTreeStopsAtWorktreeRoot(t *testing.T) {
	outer := t.TempDir()
	root := filepath.Join(outer, "worktree")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(outer, "node_modules", "vite", "package.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	probe := &packageProbe{app: root, root: root, cache: map[string]bool{}}
	if probe.installedInTree("vite") {
		t.Error("the probe must not look above the worktree root")
	}
}

func TestDeclaresPackage(t *testing.T) {
	pkg := packageJSON{
		Dependencies:    map[string]string{"next": "15.0.0"},
		DevDependencies: map[string]string{"vite": "5.4.0"},
	}
	for _, name := range []string{"next", "vite"} {
		if !declaresPackage(pkg, name) {
			t.Errorf("expected %s to be declared", name)
		}
	}
	if declaresPackage(pkg, "astro") {
		t.Error("did not expect astro to be declared")
	}
}

// With no package manager and nothing installed, the probe still reads package.json.
func TestProbeFallsBackToDeclaredDependencies(t *testing.T) {
	root := t.TempDir()
	probe := &packageProbe{
		app:   root,
		root:  root,
		pkg:   packageJSON{DevDependencies: map[string]string{"vite": "5.4.0"}},
		cache: map[string]bool{},
	}
	if !probe.has("vite") {
		t.Error("expected the declared dependency fallback to report vite")
	}
	if probe.has("next") {
		t.Error("did not expect next")
	}
}
