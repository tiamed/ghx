// Package config reads the user extension list.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const fileName = "config.json"

// File is the on-disk extension list.
type File struct {
	Extensions []Extension `json:"extensions"`
}

// Extension is one tracked GitHub repository.
type Extension struct {
	Name         string `json:"name"`
	Repo         string `json:"repo"`
	Channel      string `json:"channel,omitempty"`
	TagPattern   string `json:"tag_pattern,omitempty"`
	AssetPattern string `json:"asset_pattern,omitempty"`
	PinTag       string `json:"pin_tag,omitempty"`
	Browser      string `json:"browser,omitempty"`
}

var (
	nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	partRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// Channels understood by release selection.
var Channels = []string{"stable", "prerelease", "nightly", "preview"}

var browsers = map[string]bool{
	"":                   true,
	"chrome":             true,
	"chromium":           true,
	"edge":               true,
	"brave":              true,
	"chrome-for-testing": true,
}

// Load reads a config file. A missing file is an empty list.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &File{}, nil
	}
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// Save writes the config atomically.
func Save(path string, f *File) error {
	if f.Extensions == nil {
		f.Extensions = []Extension{}
	}
	if err := f.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Validate checks names, repos, channels, and regexes.
func (f *File) Validate() error {
	seen := map[string]bool{}
	for i := range f.Extensions {
		e := &f.Extensions[i]
		e.Channel = strings.ToLower(strings.TrimSpace(e.Channel))
		e.Browser = strings.ToLower(strings.TrimSpace(e.Browser))
		if !nameRE.MatchString(e.Name) {
			return fmt.Errorf("extension name %q is invalid", e.Name)
		}
		if seen[e.Name] {
			return fmt.Errorf("duplicate extension name %q", e.Name)
		}
		seen[e.Name] = true
		owner, repo, err := SplitRepo(e.Repo)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}
		e.Repo = owner + "/" + repo
		if e.Channel != "" && !validChannel(e.Channel) {
			return fmt.Errorf("%s: channel must be %s", e.Name, strings.Join(Channels, ", "))
		}
		if !browsers[e.Browser] {
			return fmt.Errorf("%s: unknown browser %q", e.Name, e.Browser)
		}
		if e.TagPattern != "" {
			if _, err := regexp.Compile(e.TagPattern); err != nil {
				return fmt.Errorf("%s: tag_pattern: %w", e.Name, err)
			}
		}
		if e.AssetPattern != "" {
			if _, err := regexp.Compile(e.AssetPattern); err != nil {
				return fmt.Errorf("%s: asset_pattern: %w", e.Name, err)
			}
		}
		if e.PinTag != "" && e.TagPattern != "" {
			re := regexp.MustCompile(e.TagPattern)
			if !re.MatchString(e.PinTag) {
				return fmt.Errorf("%s: pin_tag %q does not match tag_pattern", e.Name, e.PinTag)
			}
		}
	}
	return nil
}

func validChannel(c string) bool {
	for _, v := range Channels {
		if c == v {
			return true
		}
	}
	return false
}

// Find returns the extension with the given name.
func (f *File) Find(name string) (Extension, bool) {
	for _, e := range f.Extensions {
		if e.Name == name {
			return e, true
		}
	}
	return Extension{}, false
}

// SplitRepo splits "owner/repo".
func SplitRepo(repo string) (owner, name string, err error) {
	repo = strings.Trim(repo, "/")
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || !partRE.MatchString(parts[0]) || !partRE.MatchString(parts[1]) {
		return "", "", fmt.Errorf("repo must look like owner/name, got %q", repo)
	}
	return parts[0], parts[1], nil
}

// ParseRepoRef accepts owner/repo or a github.com releases URL.
// A /releases/tag/<tag> URL returns that tag as a pin.
func ParseRepoRef(raw string) (owner, repo, pin string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", fmt.Errorf("missing repository")
	}
	if !strings.Contains(raw, "://") && !strings.HasPrefix(raw, "github.com/") {
		owner, repo, err = SplitRepo(strings.TrimSuffix(raw, ".git"))
		return owner, repo, "", err
	}
	if strings.HasPrefix(raw, "github.com/") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", "", err
	}
	if u.User != nil {
		return "", "", "", fmt.Errorf("strip credentials from the URL and set GITHUB_TOKEN instead")
	}
	host := strings.ToLower(u.Host)
	if host != "github.com" && host != "www.github.com" {
		return "", "", "", fmt.Errorf("only github.com repositories are supported (%s)", u.Host)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("URL %q has no owner/repo", raw)
	}
	owner, repo = parts[0], strings.TrimSuffix(parts[1], ".git")
	if _, _, err = SplitRepo(owner + "/" + repo); err != nil {
		return "", "", "", err
	}
	if len(parts) >= 4 && parts[2] == "releases" && parts[3] == "tag" {
		if len(parts) < 5 || parts[4] == "" {
			return "", "", "", fmt.Errorf("release tag URL is missing the tag")
		}
		pin = parts[4]
	}
	return owner, repo, pin, nil
}

// Paths resolves the config file and data directory.
func Paths(goos, configFlag, dataFlag string, getenv func(string) string) (configPath, dataDir string, err error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if configFlag != "" {
		configPath = configFlag
	} else if v := getenv("GHX_CONFIG"); v != "" {
		configPath = v
	} else {
		dir, err := defaultConfigDir(goos, getenv)
		if err != nil {
			return "", "", err
		}
		configPath = filepath.Join(dir, fileName)
	}
	if dataFlag != "" {
		dataDir = dataFlag
	} else if v := getenv("GHX_DATA_DIR"); v != "" {
		dataDir = v
	} else {
		dataDir, err = defaultDataDir(goos, getenv)
		if err != nil {
			return "", "", err
		}
	}
	return configPath, dataDir, nil
}

func defaultConfigDir(goos string, getenv func(string) string) (string, error) {
	if goos == "windows" {
		base := getenv("APPDATA")
		if base == "" {
			return "", fmt.Errorf("APPDATA is not set")
		}
		return filepath.Join(base, "ghx"), nil
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "ghx"), nil
	}
	home := getenv("HOME")
	if home == "" {
		return "", fmt.Errorf("HOME is not set")
	}
	return filepath.Join(home, ".config", "ghx"), nil
}

func defaultDataDir(goos string, getenv func(string) string) (string, error) {
	if goos == "windows" {
		base := getenv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("LOCALAPPDATA is not set")
		}
		return filepath.Join(base, "ghx"), nil
	}
	if x := getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "ghx"), nil
	}
	home := getenv("HOME")
	if home == "" {
		return "", fmt.Errorf("HOME is not set")
	}
	return filepath.Join(home, ".local", "share", "ghx"), nil
}
