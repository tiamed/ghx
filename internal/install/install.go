// Package install downloads a selected release asset and unpacks it in place.
package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ghx/internal/archive"
	"ghx/internal/config"
	"ghx/internal/crxid"
	"ghx/internal/gh"
	"ghx/internal/state"
	"ghx/internal/xerr"
)

// Options controls one install or update.
type Options struct {
	DataDir string
	Client  *gh.Client
	DryRun  bool
	GOOS    string
	Stderr  io.Writer
	Pick    func(*gh.AmbiguousError) (gh.Asset, string, error)
	Now     func() time.Time
}

// Result is the outcome of checking or applying one extension.
type Result struct {
	Name             string `json:"name"`
	Repo             string `json:"repo"`
	Action           string `json:"action"`
	Tag              string `json:"tag"`
	PreviousTag      string `json:"previous_tag,omitempty"`
	Asset            string `json:"asset"`
	Version          string `json:"version,omitempty"`
	Digest           string `json:"digest,omitempty"`
	Size             int64  `json:"size,omitempty"`
	Path             string `json:"path,omitempty"`
	PathID           string `json:"path_id,omitempty"`
	CRXID            string `json:"crx_id,omitempty"`
	HTMLURL          string `json:"html_url,omitempty"`
	SuggestedPattern string `json:"suggested_pattern,omitempty"`
	Warning          string `json:"warning,omitempty"`
	Error            string `json:"error,omitempty"`
}

type cacheMeta struct {
	Digest string `json:"digest"`
	Remote string `json:"remote"`
	Size   int64  `json:"size"`
	ETag   string `json:"etag"`
}

// Apply resolves the release and, unless DryRun is set, unpacks it.
func Apply(ctx context.Context, ext config.Extension, opt Options) (Result, error) {
	if opt.Client == nil {
		return Result{}, fmt.Errorf("missing GitHub client")
	}
	if opt.GOOS == "" {
		opt.GOOS = "linux"
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if err := (&config.File{Extensions: []config.Extension{ext}}).Validate(); err != nil {
		return Result{}, err
	}
	owner, repo, err := config.SplitRepo(ext.Repo)
	if err != nil {
		return Result{}, err
	}
	rel, asset, suggested, err := resolve(ctx, opt, gh.Query{
		Owner: owner, Repo: repo, Channel: ext.Channel,
		TagPattern: ext.TagPattern, PinTag: ext.PinTag, AssetPattern: ext.AssetPattern,
	})
	if err != nil {
		return Result{}, err
	}
	unpacked := filepath.Join(opt.DataDir, "extensions", ext.Name, "unpacked")
	st, err := state.Load(statePath(opt.DataDir))
	if err != nil {
		return Result{}, err
	}
	prev, hasPrev := st.Extensions[ext.Name]
	res := Result{
		Name: ext.Name, Repo: ext.Repo, Tag: rel.Tag, Asset: asset.Name,
		Size: asset.Size, HTMLURL: rel.HTMLURL, Path: unpacked,
		SuggestedPattern: suggested,
	}
	if hasPrev {
		res.PreviousTag = prev.Tag
		res.Version = prev.Version
		res.PathID = prev.PathID
		res.CRXID = prev.CRXID
		res.Digest = prev.Digest
	}
	if upToDate(prev, hasPrev, ext, rel, asset, unpacked) {
		res.Action = "unchanged"
		res.Path = unpacked
		return res, nil
	}
	if hasPrev && prev.Tag != "" {
		res.Action = "updated"
	} else {
		res.Action = "installed"
	}
	if opt.DryRun {
		res.Action = "dry-run"
		return res, nil
	}

	unlock, err := lockDir(opt.DataDir)
	if err != nil {
		return Result{}, err
	}
	defer unlock()

	cacheFile, err := gh.CacheName(asset.Name)
	if err != nil {
		return Result{}, err
	}
	cachePath := filepath.Join(opt.DataDir, "extensions", ext.Name, "cache", cacheFile)
	meta, ok := validCache(cachePath, asset)
	if !ok {
		if opt.Stderr != nil {
			fmt.Fprintf(opt.Stderr, "downloading %s (%s)\n", asset.Name, HumanSize(asset.Size))
		}
		ifNone := ""
		if asset.Digest == "" && prev.ETag != "" && prev.Asset == asset.Name && prev.Tag == rel.Tag {
			ifNone = prev.ETag
		}
		dl, err := opt.Client.DownloadAsset(ctx, asset, cachePath, ifNone)
		if err != nil {
			return Result{}, err
		}
		if dl.NotModified && manifestExists(unpacked) {
			res.Action = "unchanged"
			return res, nil
		}
		sum256, sum512, err := opt.Client.ChecksumFor(ctx, rel, asset.Name)
		if err != nil {
			return Result{}, err
		}
		local := strings.TrimPrefix(strings.ToLower(dl.Digest), "sha256:")
		if sum256 != "" && !strings.EqualFold(sum256, local) {
			return Result{}, fmt.Errorf("%w: checksum file sha256 disagrees with %s", xerr.ErrVerify, asset.Name)
		}
		if sum512 != "" && asset.Digest != "" && strings.HasPrefix(strings.ToLower(asset.Digest), "sha512:") {
			want := strings.TrimPrefix(strings.ToLower(asset.Digest), "sha512:")
			if !strings.EqualFold(sum512, want) {
				return Result{}, fmt.Errorf("%w: checksum file sha512 disagrees with %s", xerr.ErrVerify, asset.Name)
			}
		}
		meta = cacheMeta{Digest: dl.Digest, Remote: asset.Digest, Size: dl.Size, ETag: dl.ETag}
		if err := writeMeta(cachePath, meta); err != nil {
			return Result{}, err
		}
	}

	staging := unpacked + ".staging"
	_ = os.RemoveAll(staging)
	unpackedResult, err := archive.Unpack(cachePath, staging)
	if err != nil {
		_ = os.RemoveAll(staging)
		return Result{}, err
	}
	if err := replaceDir(staging, unpacked); err != nil {
		return Result{}, err
	}
	abs, err := filepath.Abs(unpacked)
	if err != nil {
		return Result{}, err
	}
	crxID := unpackedResult.CRXID
	crxPath := prev.CRXPath
	if isCRX(asset.Name) {
		crxPath = filepath.Join(opt.DataDir, "extensions", ext.Name, "extension.crx")
		if err := copyFile(cachePath, crxPath); err != nil {
			return Result{}, err
		}
	}
	if crxID == "" {
		crxID = prev.CRXID
	}
	pathID := crxid.ForPath(opt.GOOS, abs)
	if err := writeInstallNote(filepath.Join(opt.DataDir, "extensions", ext.Name, "INSTALL.txt"), ext.Name, abs); err != nil {
		return Result{}, err
	}
	inst := state.Installed{
		Repo: ext.Repo, Tag: rel.Tag, Asset: asset.Name,
		Digest: meta.Digest, RemoteDigest: asset.Digest, Size: asset.Size, ETag: meta.ETag,
		Version: unpackedResult.Version, Path: abs, PathID: pathID, CRXID: crxID, CRXPath: crxPath,
		ExternalJSON: prev.ExternalJSON, InstalledAt: opt.Now().UTC(),
	}
	warning := ""
	if len(prev.ExternalJSON) > 0 {
		if isCRX(asset.Name) {
			if err := refreshExternal(prev.ExternalJSON, crxPath, unpackedResult.Version); err != nil {
				warning = err.Error()
			}
		} else {
			warning = "Linux external CRX JSON was not refreshed because this update downloaded a zip. Run: ghx policy " + ext.Name + " --fetch --install-external"
		}
	}
	st.Extensions[ext.Name] = inst
	if err := state.Save(statePath(opt.DataDir), st); err != nil {
		return Result{}, err
	}
	res.Action = actionName(hasPrev)
	res.Version = unpackedResult.Version
	res.Digest = meta.Digest
	res.Path = abs
	res.PathID = pathID
	res.CRXID = crxID
	res.Size = asset.Size
	res.Warning = warning
	return res, nil
}

func actionName(hasPrev bool) string {
	if hasPrev {
		return "updated"
	}
	return "installed"
}

func resolve(ctx context.Context, opt Options, q gh.Query) (gh.Release, gh.Asset, string, error) {
	rel, asset, err := opt.Client.Resolve(ctx, q)
	if err == nil {
		return rel, asset, "", nil
	}
	var amb *gh.AmbiguousError
	if errors.As(err, &amb) && opt.Pick != nil {
		asset, pattern, perr := opt.Pick(amb)
		if perr != nil {
			return gh.Release{}, gh.Asset{}, "", perr
		}
		return amb.Release, asset, pattern, nil
	}
	return gh.Release{}, gh.Asset{}, "", err
}

func upToDate(prev state.Installed, hasPrev bool, ext config.Extension, rel gh.Release, asset gh.Asset, unpacked string) bool {
	if !hasPrev || !manifestExists(unpacked) || prev.Repo != ext.Repo {
		return false
	}
	if asset.Digest != "" {
		return gh.CompareDigest(asset.Digest, prev.Digest, prev.RemoteDigest)
	}
	return prev.Tag == rel.Tag && prev.Asset == asset.Name && prev.Size == asset.Size && prev.Size > 0
}

func manifestExists(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "manifest.json"))
	return err == nil && !st.IsDir()
}

func statePath(data string) string { return filepath.Join(data, "state.json") }

func validCache(path string, asset gh.Asset) (cacheMeta, bool) {
	st, err := os.Stat(path)
	if err != nil || (asset.Size > 0 && st.Size() != asset.Size) {
		return cacheMeta{}, false
	}
	b, err := os.ReadFile(path + ".meta.json")
	if err != nil {
		return cacheMeta{}, false
	}
	var meta cacheMeta
	if json.Unmarshal(b, &meta) != nil {
		return cacheMeta{}, false
	}
	if asset.Digest != "" && !strings.EqualFold(meta.Remote, asset.Digest) && !gh.CompareDigest(asset.Digest, meta.Digest, meta.Remote) {
		return cacheMeta{}, false
	}
	if meta.Size != 0 && meta.Size != st.Size() {
		return cacheMeta{}, false
	}
	return meta, true
}

func writeMeta(path string, meta cacheMeta) error {
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path+".meta.json", b, 0o644)
}

func isCRX(name string) bool {
	low := strings.ToLower(name)
	return strings.HasSuffix(low, ".crx") || strings.HasSuffix(low, ".crx3")
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".partial"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

func replaceDir(staging, dest string) error {
	backup := dest + ".bak"
	_ = os.RemoveAll(backup)
	had := false
	if st, err := os.Stat(dest); err == nil && st.IsDir() {
		had = true
		if err := renameRetry(dest, backup); err != nil {
			return err
		}
	}
	if err := renameRetry(staging, dest); err != nil {
		if had {
			_ = renameRetry(backup, dest)
		}
		return err
	}
	_ = os.RemoveAll(backup)
	return nil
}

func renameRetry(from, to string) error {
	var err error
	for i := 0; i < 5; i++ {
		err = os.Rename(from, to)
		if err == nil {
			return nil
		}
		time.Sleep(time.Duration(40*(i+1)) * time.Millisecond)
	}
	return err
}

func lockDir(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "ghx.lock")
	var f *os.File
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		f, err = os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if !stealStaleLock(p) {
			return nil, fmt.Errorf("another ghx process holds %s", p)
		}
	}
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("another ghx process holds %s", p)
		}
		return nil, err
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	f.Close()
	return func() { _ = os.Remove(p) }, nil
}

// stealStaleLock removes a lock whose owner PID is dead, or one older than 30 minutes.
func stealStaleLock(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid > 0 && !pidAlive(pid) {
		return os.Remove(path) == nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	if time.Since(st.ModTime()) > 30*time.Minute {
		return os.Remove(path) == nil
	}
	return false
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return windowsProcessExists(uint32(pid))
}

func writeInstallNote(path, name, unpacked string) error {
	body := fmt.Sprintf(`%s

Chrome / Chromium — load this unpacked extension once:

  1. Open chrome://extensions
  2. Turn on Developer mode
  3. Click Load unpacked and choose:
     %s

After "ghx update", click Reload on the %s card. The folder path stays the same.

Branded Chrome (Windows and Linux) ignores --load-extension since Chrome 137
and will not install a local CRX from the Windows registry. Do not copy this
folder into the profile Extensions directory; Chrome ignores files dropped there.
`, name, unpacked, name)
	return os.WriteFile(path, []byte(body), 0o644)
}

// WriteExternalJSON writes a Linux external_crx preferences file.
func WriteExternalJSON(path, crxPath, version string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload := struct {
		ExternalCRX     string `json:"external_crx"`
		ExternalVersion string `json:"external_version"`
	}{ExternalCRX: crxPath, ExternalVersion: version}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func refreshExternal(paths []string, crxPath, version string) error {
	if crxPath == "" || version == "" {
		return fmt.Errorf("cannot refresh external JSON without a CRX path and version")
	}
	var first error
	for _, p := range paths {
		if err := WriteExternalJSON(p, crxPath, version); err != nil && first == nil {
			first = fmt.Errorf("update external JSON %s: %w", p, err)
		}
	}
	return first
}

// HumanSize formats a byte count for logs.
func HumanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

// RemoveFiles deletes the on-disk extension and its state entry.
func RemoveFiles(dataDir, name string) error {
	st, err := state.Load(statePath(dataDir))
	if err != nil {
		return err
	}
	delete(st.Extensions, name)
	if err := state.Save(statePath(dataDir), st); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(dataDir, "extensions", name))
}
