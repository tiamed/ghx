package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"ghx/internal/archive"
	"ghx/internal/config"
	"ghx/internal/gh"
	"ghx/internal/xerr"
)

func TestApplyUpdateSkipAndDryRun(t *testing.T) {
	v1 := zipWithVersion(t, "1.0.0")
	v2 := zipWithVersion(t, "1.0.1")
	var mu sync.Mutex
	body := v1
	digest := sha(v1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/ext/releases/latest":
			mu.Lock()
			d := digest
			n := len(body)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name":     "v1",
				"draft":        false,
				"prerelease":   false,
				"published_at": "2026-01-01T00:00:00Z",
				"html_url":     "https://github.com/acme/ext/releases/tag/v1",
				"assets": []map[string]any{{
					"name":                 "ext-chrome.zip",
					"size":                 n,
					"digest":               "sha256:" + d,
					"state":                "uploaded",
					"browser_download_url": "http://" + r.Host + "/ext-chrome.zip",
				}},
			})
		case "/ext-chrome.zip":
			mu.Lock()
			b := body
			mu.Unlock()
			_, _ = w.Write(b)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	data := t.TempDir()
	client := gh.NewClient(srv.URL, "", srv.Client())
	ext := config.Extension{Name: "ext", Repo: "acme/ext", Channel: "stable"}
	ctx := context.Background()

	dry, err := Apply(ctx, ext, Options{DataDir: data, Client: client, DryRun: true, GOOS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Action != "dry-run" || dry.Asset != "ext-chrome.zip" {
		t.Fatalf("%+v", dry)
	}
	if _, err := os.Stat(filepath.Join(data, "extensions", "ext", "unpacked", "manifest.json")); err == nil {
		t.Fatal("dry-run unpacked the extension")
	}

	got, err := Apply(ctx, ext, Options{DataDir: data, Client: client, GOOS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != "installed" || got.Version != "1.0.0" || got.PathID == "" {
		t.Fatalf("%+v", got)
	}
	man, err := os.ReadFile(filepath.Join(got.Path, "manifest.json"))
	if err != nil || len(man) == 0 {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(got.Path, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}

	again, err := Apply(ctx, ext, Options{DataDir: data, Client: client, GOOS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Action != "unchanged" {
		t.Fatalf("%+v", again)
	}
	info2, err := os.Stat(filepath.Join(got.Path, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(info2.ModTime()) {
		t.Fatal("unchanged update rewrote the unpacked extension")
	}

	mu.Lock()
	body = v2
	digest = sha(v2)
	mu.Unlock()
	upd, err := Apply(ctx, ext, Options{DataDir: data, Client: client, GOOS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Action != "updated" || upd.Version != "1.0.1" {
		t.Fatalf("%+v", upd)
	}

	mu.Lock()
	digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	// keep body as v2 but lie about the digest; size still matches v2
	mu.Unlock()
	// Force a mismatch by changing only the digest while size stays equal to v2.
	// valid cache won't match the new remote digest, download will fail verify,
	// and the unpacked tree must stay on 1.0.1.
	_, err = Apply(ctx, ext, Options{DataDir: data, Client: client, GOOS: "linux"})
	if xerr.ExitCode(err) != 4 {
		t.Fatalf("expected verify failure, got %v", err)
	}
	still, err := os.ReadFile(filepath.Join(got.Path, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(still) {
		t.Fatal("manifest corrupted")
	}
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(still, &m) != nil || m.Version != "1.0.1" {
		t.Fatalf("version rolled back or lost: %s", still)
	}
}

func zipWithVersion(t *testing.T, version string) []byte {
	t.Helper()
	b, err := archive.PackZip(map[string]string{
		"manifest.json": `{"name":"Ext","version":"` + version + `","manifest_version":3}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
