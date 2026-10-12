// Package cli is the ghx command line.
package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"ghx/internal/config"
	"ghx/internal/gh"
	"ghx/internal/redact"
	"ghx/internal/version"
	"ghx/internal/xerr"
)

// Options overrides process defaults. Zero values use the real environment.
type Options struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Getenv     func(string) string
	APIBase    string
	HTTP       *http.Client
	StdinIsTTY bool
	GOOS       string
	LookPath   func(string) (string, error)
	Exists     func(string) bool
}

// Run executes ghx and returns a process exit code.
func Run(args []string, opt Options) int {
	if opt.Stdin == nil {
		opt.Stdin = os.Stdin
	}
	if opt.Stdout == nil {
		opt.Stdout = os.Stdout
	}
	if opt.Stderr == nil {
		opt.Stderr = os.Stderr
	}
	if opt.Getenv == nil {
		opt.Getenv = os.Getenv
	}
	if opt.GOOS == "" {
		opt.GOOS = runtime.GOOS
	}
	globals, rest, err := splitGlobals(args)
	if err != nil {
		fmt.Fprintf(opt.Stderr, "ghx: %s\n", err)
		return 1
	}
	if globals.help && len(rest) == 0 {
		fmt.Fprint(opt.Stdout, rootHelp)
		return 0
	}
	cmd := ""
	var cmdArgs []string
	if len(rest) > 0 {
		cmd = rest[0]
		cmdArgs = rest[1:]
	}
	if cmd == "" || cmd == "help" || cmd == "--help" || cmd == "-h" {
		if cmd == "help" && len(cmdArgs) == 1 {
			fmt.Fprint(opt.Stdout, commandHelp(cmdArgs[0]))
			return 0
		}
		fmt.Fprint(opt.Stdout, rootHelp)
		return 0
	}
	if cmd == "version" || cmd == "--version" {
		fmt.Fprintf(opt.Stdout, "ghx %s\n", version.Version)
		return 0
	}
	tok, src := tokenFrom(opt.Getenv)
	a := &app{
		opt: opt, stdin: opt.Stdin, stdout: opt.Stdout, stderr: opt.Stderr,
		configPath: globals.config, dataDir: globals.data, verbose: globals.verbose,
		token: tok, tokenSrc: src, goos: opt.GOOS,
	}
	if err := a.resolvePaths(); err != nil {
		return a.fail(err)
	}
	switch cmd {
	case "add":
		return a.add(cmdArgs)
	case "list":
		return a.list(cmdArgs)
	case "remove", "rm":
		return a.remove(cmdArgs)
	case "update":
		return a.update(cmdArgs, false)
	case "check":
		return a.update(cmdArgs, true)
	case "path":
		return a.path(cmdArgs)
	case "launch":
		return a.launch(cmdArgs)
	case "policy":
		return a.policy(cmdArgs)
	case "doctor":
		return a.doctor(cmdArgs)
	default:
		fmt.Fprintf(a.stderr, "ghx: unknown command %q\n\n", cmd)
		fmt.Fprint(a.stderr, rootHelp)
		return 1
	}
}

type globalFlags struct {
	verbose bool
	help    bool
	config  string
	data    string
}

func splitGlobals(args []string) (globalFlags, []string, error) {
	var g globalFlags
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			g.help = true
			rest = append(rest, a)
		case a == "-v" || a == "--verbose":
			g.verbose = true
		case a == "--config":
			if i+1 >= len(args) {
				return g, nil, fmt.Errorf("--config needs a path")
			}
			i++
			g.config = args[i]
		case strings.HasPrefix(a, "--config="):
			g.config = strings.TrimPrefix(a, "--config=")
		case a == "--data":
			if i+1 >= len(args) {
				return g, nil, fmt.Errorf("--data needs a path")
			}
			i++
			g.data = args[i]
		case strings.HasPrefix(a, "--data="):
			g.data = strings.TrimPrefix(a, "--data=")
		case a == "--":
			rest = append(rest, args[i+1:]...)
			return g, rest, nil
		default:
			rest = append(rest, a)
		}
	}
	return g, rest, nil
}

func tokenFrom(getenv func(string) string) (string, string) {
	if v := strings.TrimSpace(getenv("GITHUB_TOKEN")); v != "" {
		return v, "GITHUB_TOKEN"
	}
	if v := strings.TrimSpace(getenv("GH_TOKEN")); v != "" {
		return v, "GH_TOKEN"
	}
	return "", ""
}

type app struct {
	opt         Options
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	configPath  string
	dataDir     string
	verbose     bool
	token       string
	tokenSrc    string
	goos        string
	patternName string
}

func (a *app) optHTTP() (*http.Client, bool) {
	if a.opt.HTTP != nil {
		return a.opt.HTTP, true
	}
	return nil, false
}

func (a *app) resolvePaths() error {
	cfg, data, err := config.Paths(a.goos, a.configPath, a.dataDir, a.opt.Getenv)
	if err != nil {
		return err
	}
	a.configPath, a.dataDir = cfg, data
	return nil
}

func (a *app) fail(err error) int {
	if err == nil {
		return 0
	}
	fmt.Fprintf(a.stderr, "ghx: %s\n", a.clean(err.Error()))
	return xerr.ExitCode(err)
}

func (a *app) clean(s string) string { return redact.Clean(s, a.token) }

func (a *app) logf(format string, args ...any) {
	if !a.verbose {
		return
	}
	fmt.Fprintln(a.stderr, a.clean(fmt.Sprintf(format, args...)))
}

func (a *app) client() *gh.Client {
	c := gh.NewClient(a.opt.APIBase, a.token, nil)
	if hc, ok := a.optHTTP(); ok {
		c.HTTP = hc
	}
	c.Logf = a.logf
	return c
}

func (a *app) tty() bool {
	if a.opt.StdinIsTTY {
		return true
	}
	f, ok := a.stdin.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func (a *app) pick(err *gh.AmbiguousError) (gh.Asset, string, error) {
	if !a.tty() {
		return gh.Asset{}, "", err
	}
	fmt.Fprintln(a.stderr, err.Error())
	fmt.Fprintln(a.stderr, "Enter the number of the asset to track.")
	in := bufio.NewScanner(a.stdin)
	for {
		fmt.Fprint(a.stderr, "> ")
		if !in.Scan() {
			return gh.Asset{}, "", err
		}
		var n int
		if _, scanErr := fmt.Sscan(strings.TrimSpace(in.Text()), &n); scanErr != nil || n < 1 || n > len(err.Assets) {
			fmt.Fprintf(a.stderr, "Enter a number from 1 to %d\n", len(err.Assets))
			continue
		}
		asset := err.Assets[n-1]
		pattern := gh.VersionedPattern(asset.Name)
		fmt.Fprintf(a.stderr, "Using %s\nSaved asset_pattern %s\n", asset.Name, pattern)
		if saveErr := a.rememberPattern(pattern); saveErr != nil {
			return gh.Asset{}, "", saveErr
		}
		return asset, pattern, nil
	}
}

// rememberPattern is set by update/add while an extension name is in scope.
func (a *app) rememberPattern(pattern string) error {
	if a.patternName == "" || pattern == "" {
		return nil
	}
	f, err := config.Load(a.configPath)
	if err != nil {
		return err
	}
	for i := range f.Extensions {
		if f.Extensions[i].Name == a.patternName {
			f.Extensions[i].AssetPattern = pattern
		}
	}
	return config.Save(a.configPath, f)
}

func reorder(args []string, boolFlags map[string]bool) ([]string, error) {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if a == "-" || !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
		if strings.Contains(a, "=") || boolFlags[name] {
			continue
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("flag --%s needs a value", name)
		}
		i++
		flags = append(flags, args[i])
	}
	return append(flags, pos...), nil
}

func (a *app) newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	fs.Usage = func() { fmt.Fprint(a.stderr, commandHelp(name)) }
	return fs
}

func versionString() string { return version.Version }
