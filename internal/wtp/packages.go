package wtp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// packageQueryTimeout bounds a single package-manager query.
// A slow or hanging package manager must not block a preview launch.
const packageQueryTimeout = 10 * time.Second

// packageQuery describes how to ask one package manager whether a package is installed.
type packageQuery struct {
	// arguments are passed to the package manager, with %s replaced by the package name.
	arguments []string
	// pathOutput marks output whose lines are filesystem paths into node_modules.
	pathOutput bool
}

// packageProbe answers whether a package is installed for the target app.
//
// It asks the package manager first. The package manager resolves transitive and
// workspace-hoisted packages that the package.json dependency maps never list.
// When the package manager cannot answer, the probe falls back to the installed
// node_modules tree, and finally to the declared package.json dependencies.
type packageProbe struct {
	manager string
	app     string
	root    string
	pkg     packageJSON
	cache   map[string]bool
}

func newPackageProbe(target Target) *packageProbe {
	return &packageProbe{
		manager: target.PackageManager,
		app:     target.TargetApp,
		root:    target.TargetWorktree,
		pkg:     target.pkg,
		cache:   map[string]bool{},
	}
}

func (p *packageProbe) has(name string) bool {
	if cached, found := p.cache[name]; found {
		return cached
	}
	present := p.lookup(name)
	p.cache[name] = present
	return present
}

func (p *packageProbe) lookup(name string) bool {
	if present, answered := p.queryPackageManager(name); answered {
		return present
	}
	if p.installedInTree(name) {
		return true
	}
	return declaresPackage(p.pkg, name)
}

// queryPackageManager runs the package manager's own dependency query.
// The second result reports whether the package manager produced a usable answer.
func (p *packageProbe) queryPackageManager(name string) (present bool, answered bool) {
	query, supported := packageQueryFor(p.manager)
	if !supported {
		return false, false
	}

	arguments := make([]string, 0, len(query.arguments))
	for _, argument := range query.arguments {
		arguments = append(arguments, strings.ReplaceAll(argument, "%s", name))
	}

	ctx, cancel := context.WithTimeout(context.Background(), packageQueryTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, p.manager, arguments...)
	command.Dir = p.app
	command.Stdin = nil
	output, err := command.Output()
	if ctx.Err() != nil {
		return false, false
	}
	// A missing package makes several package managers exit non-zero while still
	// printing a usable tree, so an exit status alone is not an answer.
	var exitError *exec.ExitError
	if err != nil && !errors.As(err, &exitError) {
		return false, false
	}
	return queryOutputHasPackage(string(output), name, query.pathOutput), true
}

func packageQueryFor(manager string) (packageQuery, bool) {
	switch manager {
	case "pnpm":
		return packageQuery{arguments: []string{"list", "--depth", "Infinity", "--parseable", "%s"}, pathOutput: true}, true
	case "npm":
		return packageQuery{arguments: []string{"ls", "%s", "--all", "--parseable"}, pathOutput: true}, true
	case "bun":
		return packageQuery{arguments: []string{"pm", "ls", "--all"}}, true
	default:
		// Yarn Classic and Yarn Berry disagree on both command and output shape.
		// The node_modules fallback covers them without guessing a Yarn version.
		return packageQuery{}, false
	}
}

// queryOutputHasPackage reports whether a package-manager query names the package.
func queryOutputHasPackage(output, name string, pathOutput bool) bool {
	segment := "node_modules/" + name
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if pathOutput {
			normalized := filepath.ToSlash(line)
			if strings.HasSuffix(normalized, segment) || strings.Contains(normalized, segment+"/") {
				return true
			}
			continue
		}
		// Tree output prefixes entries with drawing characters and spaces.
		entry := strings.TrimLeft(line, "│├└─| \t")
		if strings.HasPrefix(entry, name+"@") {
			return true
		}
	}
	return false
}

// installedInTree looks for the package in every node_modules directory between
// the app directory and the worktree root. This covers hoisted monorepo installs.
func (p *packageProbe) installedInTree(name string) bool {
	directory := p.app
	for {
		manifest := filepath.Join(directory, "node_modules", filepath.FromSlash(name), "package.json")
		if _, err := os.Stat(manifest); err == nil {
			return true
		}
		if directory == p.root {
			return false
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return false
		}
		directory = parent
	}
}

// declaresPackage reports whether package.json lists the package directly.
func declaresPackage(pkg packageJSON, name string) bool {
	if _, found := pkg.Dependencies[name]; found {
		return true
	}
	_, found := pkg.DevDependencies[name]
	return found
}
