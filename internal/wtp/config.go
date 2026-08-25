package wtp

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	AppDir          string
	Port            int
	PackageManager  string
	InstallCommand  string
	InstallDisabled bool
	StartCommand    string
	EnvFiles        []string
	Shell           string
}

type Repository struct {
	Root      string
	CommonDir string
	Config    Config
}

type packageJSON struct {
	PackageManager  string            `json:"packageManager"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

type Target struct {
	Repository     string
	CommonDir      string
	MainWorktree   string
	TargetWorktree string
	MainApp        string
	TargetApp      string
	Label          string
	Branch         string
	Port           int
	PackageManager string
	InstallCommand string
	StartCommand   string
	EnvFiles       []string
	AppDir         string
	Shell          string

	// pkg is the parsed target package.json, reused by deferred start detection.
	pkg packageJSON
}

var (
	viteCommand = regexp.MustCompile(`(^|[[:space:]&;|])([^[:space:]&;|]*/)?vite([[:space:]]|$)`)
	nextCommand = regexp.MustCompile(`(^|[[:space:]&;|])([^[:space:]&;|]*/)?next([[:space:]]|$)`)
	craCommand  = regexp.MustCompile(`(^|[[:space:]&;|])([^[:space:]&;|]*/)?react-scripts([[:space:]]|$)`)
)

func loadRepository() (*Repository, error) {
	root, err := gitOutput("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("run from a Git worktree")
	}
	root, err = canonicalPath(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	commonDirValue, err := gitOutputAt(root, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("resolve common Git directory: %w", err)
	}
	if !filepath.IsAbs(commonDirValue) {
		commonDirValue = filepath.Join(root, commonDirValue)
	}
	commonDir, err := canonicalPath(commonDirValue)
	if err != nil {
		return nil, fmt.Errorf("resolve common Git directory: %w", err)
	}

	config, err := loadConfig(root)
	if err != nil {
		return nil, err
	}
	return &Repository{Root: root, CommonDir: commonDir, Config: config}, nil
}

func loadConfig(root string) (Config, error) {
	appDir := configValue(root, "worktreePreview.appDir")
	if appDir == "" {
		appDir = "."
	}
	cleanAppDir, err := safeRelativePath(appDir)
	if err != nil {
		return Config{}, fmt.Errorf("unsafe appDir %q: %w", appDir, err)
	}

	port := 3000
	if rawPort := configValue(root, "worktreePreview.port"); rawPort != "" {
		port, err = strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("invalid preview port: %s", rawPort)
		}
	}

	manager := configValue(root, "worktreePreview.packageManager")
	if manager != "" && !supportedPackageManager(manager) {
		return Config{}, fmt.Errorf("unsupported package manager: %s", manager)
	}
	install := configValue(root, "worktreePreview.install")
	installDisabled := install == "false" || install == "none"
	start := configValue(root, "worktreePreview.start")
	if err := validateCommand("worktreePreview.install", install); err != nil {
		return Config{}, err
	}
	if err := validateCommand("worktreePreview.start", start); err != nil {
		return Config{}, err
	}
	if installDisabled {
		install = ""
	}

	shell := configValue(root, "worktreePreview.shell")
	if shell == "" {
		shell = "/bin/sh"
	}
	resolvedShell, err := exec.LookPath(shell)
	if err != nil {
		return Config{}, fmt.Errorf("preview shell is not executable: %s", shell)
	}

	return Config{
		AppDir:          cleanAppDir,
		Port:            port,
		PackageManager:  manager,
		InstallCommand:  install,
		InstallDisabled: installDisabled,
		StartCommand:    start,
		EnvFiles:        configValues(root, "worktreePreview.envFile"),
		Shell:           resolvedShell,
	}, nil
}

func (r *Repository) prepareTarget(branch string) (Target, error) {
	mainWorktree, targetWorktree, err := resolveWorktrees(r.Root, branch)
	if err != nil {
		return Target{}, err
	}
	mainApp, err := canonicalPath(filepath.Join(mainWorktree, r.Config.AppDir))
	if err != nil {
		return Target{}, fmt.Errorf("main app directory missing: %s", r.Config.AppDir)
	}
	targetApp, err := canonicalPath(filepath.Join(targetWorktree, r.Config.AppDir))
	if err != nil {
		return Target{}, fmt.Errorf("target app directory missing: %s", r.Config.AppDir)
	}
	if !pathWithin(targetWorktree, targetApp) {
		return Target{}, fmt.Errorf("appDir escapes target worktree")
	}

	pkg, err := readPackageJSON(filepath.Join(targetApp, "package.json"))
	if err != nil && !os.IsNotExist(err) {
		return Target{}, err
	}
	manager := r.Config.PackageManager
	if manager == "" {
		manager = detectPackageManager(targetApp, pkg)
	}
	install := r.Config.InstallCommand
	if install == "" && !r.Config.InstallDisabled {
		install = defaultInstallCommand(manager)
	}
	return Target{
		Repository:     mainWorktree,
		CommonDir:      r.CommonDir,
		MainWorktree:   mainWorktree,
		TargetWorktree: targetWorktree,
		MainApp:        mainApp,
		TargetApp:      targetApp,
		Label:          filepath.Base(targetWorktree),
		Branch:         branch,
		Port:           r.Config.Port,
		PackageManager: manager,
		InstallCommand: install,
		StartCommand:   r.Config.StartCommand,
		EnvFiles:       append([]string(nil), r.Config.EnvFiles...),
		AppDir:         r.Config.AppDir,
		Shell:          r.Config.Shell,
		pkg:            pkg,
	}, nil
}

func readPackageJSON(path string) (packageJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return packageJSON{}, err
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return packageJSON{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if pkg.Scripts == nil {
		pkg.Scripts = map[string]string{}
	}
	return pkg, nil
}

func detectPackageManager(app string, pkg packageJSON) string {
	if declared := strings.SplitN(pkg.PackageManager, "@", 2)[0]; supportedPackageManager(declared) {
		return declared
	}
	checks := []struct {
		manager string
		files   []string
	}{
		{"pnpm", []string{"pnpm-lock.yaml"}},
		{"npm", []string{"package-lock.json", "npm-shrinkwrap.json"}},
		{"yarn", []string{"yarn.lock"}},
		{"bun", []string{"bun.lock", "bun.lockb"}},
	}
	for _, check := range checks {
		for _, name := range check.files {
			if _, err := os.Stat(filepath.Join(app, name)); err == nil {
				return check.manager
			}
		}
	}
	return ""
}

// ResolveStartCommand fills StartCommand when the repository configures none.
//
// Call it after dependencies are installed. Detection asks the package manager
// which frameworks are present, and the package manager can only answer once
// node_modules exists.
func (t *Target) ResolveStartCommand() error {
	if t.StartCommand != "" {
		return nil
	}
	command, err := detectStartCommand(t.PackageManager, t.pkg, newPackageProbe(*t).has)
	if err != nil {
		return err
	}
	t.StartCommand = command
	return nil
}

// detectStartCommand picks a dev-server command for the target app.
// installed reports whether a package is available to the target app.
func detectStartCommand(manager string, pkg packageJSON, installed func(string) bool) (string, error) {
	if manager == "" {
		return "", fmt.Errorf("could not detect start command; set git config worktreePreview.start")
	}
	if script := pkg.Scripts["worktree-preview"]; script != "" {
		return packageScriptCommand(manager, "worktree-preview", ""), nil
	}
	dev := pkg.Scripts["dev"]
	start := pkg.Scripts["start"]
	if installed("vite") && viteCommand.MatchString(dev) && !strings.Contains(dev, "--open") {
		return packageScriptCommand(manager, "dev", `--port "$WORKTREE_PREVIEW_PORT" --strictPort`), nil
	}
	if installed("vite") && viteCommand.MatchString(start) && !strings.Contains(start, "--open") {
		return packageScriptCommand(manager, "start", `--port "$WORKTREE_PREVIEW_PORT" --strictPort`), nil
	}
	if installed("next") && nextCommand.MatchString(dev) {
		return packageScriptCommand(manager, "dev", `--port "$WORKTREE_PREVIEW_PORT"`), nil
	}
	if installed("react-scripts") && craCommand.MatchString(start) {
		return packageScriptCommand(manager, "start", ""), nil
	}
	return "", fmt.Errorf("could not detect a safe start command; add a package.json worktree-preview script or set git config worktreePreview.start")
}

func packageScriptCommand(manager, script, args string) string {
	var command string
	switch manager {
	case "pnpm":
		command = "pnpm run " + script
	case "npm":
		command = "npm run " + script
		if args != "" {
			command += " --"
		}
	case "yarn":
		command = "yarn " + script
	case "bun":
		command = "bun run " + script
	}
	if args != "" {
		command += " " + args
	}
	return command
}

func defaultInstallCommand(manager string) string {
	switch manager {
	case "pnpm":
		return "pnpm install --frozen-lockfile"
	case "npm":
		return "npm ci"
	case "yarn":
		return "yarn install --immutable"
	case "bun":
		return "bun install --frozen-lockfile"
	default:
		return ""
	}
}

func supportedPackageManager(manager string) bool {
	switch manager {
	case "pnpm", "npm", "yarn", "bun":
		return true
	default:
		return false
	}
}

func validateCommand(name, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must be a single-line command", name)
	}
	return nil
}

func safeRelativePath(value string) (string, error) {
	if value == "" || filepath.IsAbs(value) {
		return "", fmt.Errorf("must be a relative path")
	}
	clean := filepath.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("must not leave the worktree")
	}
	return clean, nil
}

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}
