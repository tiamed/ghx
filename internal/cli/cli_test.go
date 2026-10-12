package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ghx/internal/archive"
)

func TestHelpAndUnknown(t *testing.T) {
	var out, err bytes.Buffer
	if code := Run([]string{"--help"}, Options{Stdout: &out, Stderr: &err}); code != 0 {
		t.Fatal(code, err.String())
	}
	for _, s := range []string{"add", "update", "list", "remove", "Exit codes", "GITHUB_TOKEN"} {
		if !strings.Contains(out.String(), s) {
			t.Fatalf("help missing %q\n%s", s, out.String())
		}
	}
	out.Reset()
	err.Reset()
	if code := Run([]string{"nope"}, Options{Stdout: &out, Stderr: &err}); code != 1 {
		t.Fatal(code)
	}
	if code := Run([]string{"help", "add"}, Options{Stdout: &out, Stderr: &err}); code != 0 || !strings.Contains(out.String(), "--channel") {
		t.Fatalf("help add: %d %s", code, out.String())
	}
}

func TestTrackListRemove(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	data := filepath.Join(dir, "data")
	var out, err bytes.Buffer
	opt := Options{Stdout: &out, Stderr: &err, GOOS: "linux"}
	args := func(a ...string) []string {
		return append([]string{"--config", cfg, "--data", data}, a...)
	}
	if code := Run(args("add", "tiamed/collectab", "--track-only"), opt); code != 0 {
		t.Fatal(code, err.String())
	}
	if !strings.Contains(out.String(), "tracking collectab") {
		t.Fatal(out.String())
	}
	out.Reset()
	if code := Run(args("list"), opt); code != 0 {
		t.Fatal(code, err.String())
	}
	if !strings.Contains(out.String(), "tiamed/collectab") || !strings.Contains(out.String(), "not installed") {
		t.Fatal(out.String())
	}
	out.Reset()
	err.Reset()
	// Pretend a prior install finished so a second add still needs --force.
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(data, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"extensions":{"collectab":{"repo":"tiamed/collectab","tag":"v1","asset":"x.zip","size":1,"path":"x","installed_at":"2026-01-01T00:00:00Z"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := Run(args("add", "tiamed/collectab"), opt); code != 1 {
		t.Fatal("expected duplicate to fail", code, err.String())
	}
	if !strings.Contains(err.String(), "--force") {
		t.Fatal(err.String())
	}
	_ = os.Remove(statePath)
	out.Reset()
	err.Reset()
	if code := Run(args("remove", "missing"), opt); code != 3 {
		t.Fatal(code, err.String())
	}
	out.Reset()
	if code := Run(args("remove", "collectab", "--keep-files"), opt); code != 0 {
		t.Fatal(code, err.String())
	}
	if !strings.Contains(out.String(), "chrome://extensions") {
		t.Fatal(out.String())
	}
	out.Reset()
	if code := Run(args("list"), opt); code != 0 || !strings.Contains(out.String(), "No extensions") {
		t.Fatal(out.String())
	}
}

func TestUpdateIdempotentAndRedact(t *testing.T) {
	payload, err := archive.PackZip(map[string]string{
		"manifest.json": `{"name":"Collectab","version":"0.2.18","manifest_version":3}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	const token = "ghp_SUPERSECRETTOKENVALUE"
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/tiamed/collectab/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v0.2.18", "draft": false, "prerelease": false,
			"published_at": "2026-08-27T11:01:21Z",
			"html_url":     "https://github.com/tiamed/collectab/releases/tag/v0.2.18",
			"assets": []map[string]any{{
				"name": "collectab-0.2.18-chrome.zip", "size": len(payload),
				"digest": "sha256:" + digest, "state": "uploaded",
				"browser_download_url": "",
				"url":                  srv.URL + "/assets/1",
			}},
		})
	})
	mux.HandleFunc("/assets/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			// Redirects must not be required for this path; the first hop is authenticated.
			http.Error(w, "missing auth "+token, http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, srv.URL+"/files/ext.zip", http.StatusFound)
	})
	mux.HandleFunc("/files/ext.zip", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Authorization"), token) {
			t.Errorf("token forwarded to file host")
		}
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/repos/acme/missing/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found ` + token + `"}`))
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	data := filepath.Join(dir, "data")
	var out, errb bytes.Buffer
	opt := Options{
		Stdout: &out, Stderr: &errb, GOOS: "linux", APIBase: srv.URL, HTTP: srv.Client(),
		Getenv: func(k string) string {
			if k == "GITHUB_TOKEN" {
				return token
			}
			return ""
		},
	}
	base := []string{"-v", "--config", cfg, "--data", data}
	if code := Run(append(base, "add", "tiamed/collectab", "--asset", `chrome\.zip$`), opt); code != 0 {
		t.Fatal(code, errb.String(), out.String())
	}
	if strings.Contains(out.String()+errb.String(), token) || strings.Contains(out.String()+errb.String(), "SUPERSECRET") {
		t.Fatalf("token leaked\n%s\n%s", out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "installed collectab") || !strings.Contains(out.String(), "0.2.18") {
		t.Fatal(out.String())
	}
	if !strings.Contains(errb.String(), "authorization=present") {
		t.Fatal(errb.String())
	}
	man, err := os.ReadFile(filepath.Join(data, "extensions", "collectab", "unpacked", "manifest.json"))
	if err != nil || !bytes.Contains(man, []byte("0.2.18")) {
		t.Fatal(err, string(man))
	}
	out.Reset()
	errb.Reset()
	if code := Run(append(base, "update", "collectab"), opt); code != 0 {
		t.Fatal(code, errb.String())
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Fatal(out.String())
	}
	out.Reset()
	if code := Run(append(base, "update", "collectab", "--dry-run"), opt); code != 0 || !strings.Contains(out.String(), "up to date") {
		t.Fatal(code, out.String(), errb.String())
	}
	out.Reset()
	errb.Reset()
	if code := Run(append(base, "add", "acme/missing", "--name", "missing", "--track-only"), opt); code != 0 {
		t.Fatal(code, errb.String())
	}
	out.Reset()
	errb.Reset()
	if code := Run(append(base, "update"), opt); code != 5 {
		t.Fatalf("batch code %d\n%s\n%s", code, out.String(), errb.String())
	}
	if strings.Contains(errb.String(), token) {
		t.Fatal(errb.String())
	}
	out.Reset()
	errb.Reset()
	if code := Run(append(base, "doctor"), opt); code != 0 {
		t.Fatal(code, errb.String())
	}
	if !strings.Contains(out.String(), "GITHUB_TOKEN is set") || strings.Contains(out.String(), token) {
		t.Fatal(out.String())
	}
	if !strings.Contains(out.String(), "Developer mode") || !strings.Contains(out.String(), "Chrome 137") {
		t.Fatal("doctor missing sideload limits")
	}
}

func TestAmbiguousExit(t *testing.T) {
	body := []byte("not-downloaded")
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/a/b/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v1", "published_at": time.Now().UTC().Format(time.RFC3339),
			"assets": []map[string]any{
				{"name": "ext-chrome-beta.zip", "size": len(body), "state": "uploaded", "browser_download_url": "http://example.invalid/a"},
				{"name": "ext-chrome-stable.zip", "size": len(body), "state": "uploaded", "browser_download_url": "http://example.invalid/b"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	dir := t.TempDir()
	var out, errb bytes.Buffer
	opt := Options{Stdout: &out, Stderr: &errb, APIBase: srv.URL, HTTP: srv.Client(), GOOS: "linux"}
	code := Run([]string{"--config", filepath.Join(dir, "c.json"), "--data", filepath.Join(dir, "d"), "add", "a/b"}, opt)
	if code != 2 {
		t.Fatalf("code %d %s %s", code, out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "ext-chrome-beta.zip") {
		t.Fatal(errb.String())
	}
}

func TestLaunchAndPolicy(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "c.json")
	data := filepath.Join(dir, "d")
	// Pretend an extension is installed.
	unpacked := filepath.Join(data, "extensions", "ext", "unpacked")
	if err := os.MkdirAll(unpacked, 0o755); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(unpacked)
	if err != nil {
		t.Fatal(err)
	}
	st := `{"extensions":{"ext":{"repo":"a/b","tag":"v1","asset":"a.zip","size":1,"version":"1.0.0","path":"` + abs + `"}}}`
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "state.json"), []byte(st), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(`{"extensions":[{"name":"ext","repo":"a/b","channel":"stable"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	opt := Options{
		Stdout: &out, Stderr: &errb, GOOS: "linux",
		LookPath: func(name string) (string, error) {
			if name == "chromium" {
				return "/bin/true", nil
			}
			return "", errors.New("missing")
		},
		Exists: func(string) bool { return false },
	}
	code := Run([]string{"--config", cfg, "--data", data, "launch", "ext"}, opt)
	if code != 0 {
		t.Fatal(code, errb.String(), out.String())
	}
	if !strings.Contains(out.String(), "started chromium") {
		t.Fatal(out.String(), errb.String())
	}
	out.Reset()
	errb.Reset()
	opt.GOOS = "windows"
	code = Run([]string{"--config", cfg, "--data", data, "policy", "ext", "--install-external"}, opt)
	if code != 1 || !strings.Contains(errb.String(), "Windows") {
		t.Fatalf("%d %s %s", code, out.String(), errb.String())
	}
}
