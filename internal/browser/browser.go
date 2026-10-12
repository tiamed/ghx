// Package browser knows where Chrome-family browsers live and which install
// methods each one actually accepts.
package browser

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Browser is a detected Chromium-family binary.
type Browser struct {
	Name   string // chrome, chromium, edge, brave, chrome-for-testing
	Path   string
	Load   string // yes, no, unknown
	Data   string // user data dir, informational
	Policy string // managed policy directory or registry key, informational
}

// LoadExtension reports whether --load-extension is expected to work.
func (b Browser) LoadExtension() bool { return b.Load == "yes" }

// Env is the filesystem and PATH view used to find browsers.
type Env struct {
	GOOS     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Exists   func(string) bool
	Home     string
}

// Detect finds installed browsers. Missing browsers are omitted.
func Detect(env Env) []Browser {
	if env.Getenv == nil {
		env.Getenv = func(string) string { return "" }
	}
	if env.LookPath == nil {
		env.LookPath = exec.LookPath
	}
	if env.Exists == nil {
		env.Exists = func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		}
	}
	seen := map[string]bool{}
	var out []Browser
	add := func(b Browser) {
		if b.Path == "" || seen[b.Path] {
			return
		}
		seen[b.Path] = true
		out = append(out, b)
	}
	for _, spec := range specs(env) {
		for _, bin := range spec.bins {
			p, err := env.LookPath(bin)
			if err == nil {
				add(classify(env, spec.kind, p))
			}
		}
		for _, p := range spec.paths {
			if env.Exists(p) {
				add(classify(env, spec.kind, p))
			}
		}
	}
	return out
}

type spec struct {
	kind  string
	bins  []string
	paths []string
}

func specs(env Env) []spec {
	if env.GOOS == "windows" {
		roots := []string{
			env.Getenv("ProgramFiles"),
			env.Getenv("ProgramFiles(x86)"),
			env.Getenv("LOCALAPPDATA"),
		}
		var chrome, chromium, edge, brave, cft []string
		for _, root := range roots {
			if root == "" {
				continue
			}
			chrome = append(chrome, join("windows", root, `Google\Chrome\Application\chrome.exe`))
			chromium = append(chromium, join("windows", root, `Chromium\Application\chrome.exe`))
			edge = append(edge, join("windows", root, `Microsoft\Edge\Application\msedge.exe`))
			brave = append(brave, join("windows", root, `BraveSoftware\Brave-Browser\Application\brave.exe`))
			cft = append(cft, join("windows", root, `Google\Chrome for Testing\chrome.exe`))
		}
		return []spec{
			{kind: "chrome", bins: []string{"chrome.exe"}, paths: chrome},
			{kind: "chromium", bins: []string{"chromium.exe"}, paths: chromium},
			{kind: "edge", bins: []string{"msedge.exe"}, paths: edge},
			{kind: "brave", bins: []string{"brave.exe"}, paths: brave},
			{kind: "chrome-for-testing", bins: nil, paths: cft},
		}
	}
	home := env.Home
	return []spec{
		{kind: "chrome", bins: []string{"google-chrome-stable", "google-chrome"}, paths: []string{"/opt/google/chrome/chrome", "/usr/bin/google-chrome-stable", "/usr/bin/google-chrome"}},
		{kind: "chromium", bins: []string{"chromium", "chromium-browser"}, paths: []string{"/usr/bin/chromium", "/usr/bin/chromium-browser", "/snap/bin/chromium", join("linux", home, "snap/chromium/current/usr/lib/chromium-browser/chromium-browser")}},
		{kind: "edge", bins: []string{"microsoft-edge", "microsoft-edge-stable"}, paths: []string{"/usr/bin/microsoft-edge", "/usr/bin/microsoft-edge-stable", "/opt/microsoft/msedge/msedge"}},
		{kind: "brave", bins: []string{"brave-browser", "brave"}, paths: []string{"/usr/bin/brave-browser", "/opt/brave.com/brave/brave"}},
		{kind: "chrome-for-testing", bins: []string{"chrome-for-testing"}, paths: nil},
	}
}

func classify(env Env, kind, path string) Browser {
	low := strings.ToLower(path)
	name := kind
	load := "unknown"
	switch {
	case strings.Contains(low, "chrome-for-testing") || strings.Contains(low, "chrome for testing"):
		name = "chrome-for-testing"
		load = "yes"
	case strings.Contains(low, "chromium"):
		name = "chromium"
		load = "yes"
	case strings.Contains(low, "brave"):
		name = "brave"
		load = "unknown"
	case strings.Contains(low, "edge") || strings.Contains(low, "msedge"):
		name = "edge"
		load = "unknown"
	default:
		if kind == "chromium" {
			name = "chromium"
			load = "yes"
		} else if kind == "chrome-for-testing" {
			name = "chrome-for-testing"
			load = "yes"
		} else if kind == "edge" {
			name = "edge"
			load = "unknown"
		} else if kind == "brave" {
			name = "brave"
			load = "unknown"
		} else {
			name = "chrome"
			load = "no"
		}
	}
	return Browser{Name: name, Path: path, Load: load, Data: dataDir(env, name), Policy: policyDir(env.GOOS, name)}
}

func dataDir(env Env, name string) string {
	if env.GOOS == "windows" {
		base := env.Getenv("LOCALAPPDATA")
		switch name {
		case "edge":
			return join("windows", base, `Microsoft\Edge\User Data`)
		case "brave":
			return join("windows", base, `BraveSoftware\Brave-Browser\User Data`)
		case "chromium":
			return join("windows", base, `Chromium\User Data`)
		default:
			return join("windows", base, `Google\Chrome\User Data`)
		}
	}
	home := env.Home
	switch name {
	case "edge":
		return join("linux", home, ".config/microsoft-edge")
	case "brave":
		return join("linux", home, ".config/BraveSoftware/Brave-Browser")
	case "chromium":
		snap := join("linux", home, "snap/chromium/common/chromium")
		return join("linux", home, ".config/chromium") + " (snap: " + snap + ")"
	default:
		return join("linux", home, ".config/google-chrome")
	}
}

func policyDir(goos, name string) string {
	if goos == "windows" {
		switch name {
		case "edge":
			return `HKLM\Software\Policies\Microsoft\Edge`
		default:
			return `HKLM\Software\Policies\Google\Chrome and HKCU\Software\Policies\Google\Chrome`
		}
	}
	switch name {
	case "chromium":
		return "/etc/chromium/policies/managed"
	case "edge":
		return "/etc/opt/edge/policies/managed"
	default:
		return "/etc/opt/chrome/policies/managed"
	}
}

// ExternalDirs are the Linux preferences directories for a local CRX.
// Windows has no equivalent that accepts a local CRX.
func ExternalDirs(goos, browser string) []string {
	if goos == "windows" {
		return nil
	}
	switch browser {
	case "chromium":
		return []string{
			"/usr/lib/chromium/extensions",
			"/usr/lib/chromium-browser/extensions",
			"/usr/share/chromium/extensions",
		}
	case "edge":
		return []string{"/usr/share/microsoft-edge/extensions"}
	default:
		return []string{
			"/opt/google/chrome/extensions",
			"/usr/share/google-chrome/extensions",
		}
	}
}

// LaunchPlan builds argv for a browser.
// mode is "load-extension" or "extensions-page".
func LaunchPlan(b Browser, paths []string, workaround bool) (args []string, mode, note string, err error) {
	for _, p := range paths {
		if strings.Contains(p, ",") {
			return nil, "", "", fmt.Errorf("path %s contains a comma; launch one extension at a time", p)
		}
	}
	useLoad := b.LoadExtension() || (workaround && b.Name == "chrome")
	if useLoad && len(paths) > 0 {
		args = []string{"--load-extension=" + strings.Join(paths, ",")}
		mode = "load-extension"
		if workaround && b.Name == "chrome" {
			args = append([]string{"--disable-features=DisableLoadExtensionCommandLineSwitch"}, args...)
			note = "Passing the unsupported DisableLoadExtensionCommandLineSwitch workaround. Branded Chrome may ignore it."
		}
		return args, mode, note, nil
	}
	note = "This browser will not load the extension from --load-extension. Opening chrome://extensions so you can use Load unpacked."
	return []string{"chrome://extensions"}, "extensions-page", note, nil
}

func join(goos string, parts ...string) string {
	sep := "/"
	if goos == "windows" {
		sep = `\`
	}
	var clean []string
	for _, p := range parts {
		p = strings.Trim(p, `/\`)
		if p != "" {
			clean = append(clean, p)
		}
	}
	if goos != "windows" && len(parts) > 0 && strings.HasPrefix(parts[0], "/") {
		return "/" + strings.Join(clean, sep)
	}
	return strings.Join(clean, sep)
}

// Explain is the short install-limit text printed by doctor and policy.
func Explain(goos string) string {
	return fmt.Sprintf(`How Chrome actually installs extensions that are not in the Chrome Web Store (%s):

  Load unpacked (Windows and Linux, branded Chrome and Chromium)
    Works. Enable Developer mode at chrome://extensions and choose the unpacked
    folder once. ghx keeps that folder stable, so later updates replace the files
    in place. Click Reload on the extension card after an update. Chrome may show
    a "Disable developer mode extensions" prompt; ghx does not try to hide it.

  --load-extension
    Works in Chromium and Chrome for Testing. Removed from branded Chrome in
    Chrome 137 on Windows and Linux. Edge and Brave may ignore it.

  Linux external CRX (preferences JSON)
    Documented for /opt/google/chrome/extensions and
    /usr/share/google-chrome/extensions. The JSON names a local .crx plus the
    manifest version, and the extension id must be the CRX's real key id.
    Needs root. Windows and macOS do not accept a local CRX this way: external
    installs there must come from the Chrome Web Store.

  Profile Extensions folder
    Copying files into the user-data Extensions directory does not install
    anything. Chrome only trusts entries recorded in its preferences.

  Enterprise policy (ExtensionInstallForcelist)
    An admin can force-install an extension, including a self-hosted CRX, but
    the update URL has to be http or https and ExtensionInstallSources must
    allow that host. A file:// URL is not enough. ghx does not start a server
    and does not edit enterprise policy unless you explicitly install the Linux
    external CRX JSON.

ghx's default is the unpacked folder. That is the method branded Chrome still
allows for an extension that was never uploaded to the Web Store.`, goos)
}
