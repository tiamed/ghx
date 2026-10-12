package gh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"ghx/internal/xerr"
)

func TestParseAndResolveLatest(t *testing.T) {
	mux := http.NewServeMux()
	var sawAuth string
	mux.HandleFunc("/repos/tiamed/collectab/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if r.Header.Get("Accept") == "" {
			t.Errorf("missing accept")
		}
		_, _ = io.WriteString(w, `{
		  "tag_name": "v0.2.18",
		  "name": "v0.2.18",
		  "draft": false,
		  "prerelease": false,
		  "published_at": "2026-08-27T11:01:21Z",
		  "html_url": "https://github.com/tiamed/collectab/releases/tag/v0.2.18",
		  "assets": [{
		    "id": 1,
		    "name": "collectab-0.2.18-chrome.zip",
		    "size": 1637982,
		    "digest": "sha256:fbc4558d400cfe852ae045088db2035f442ca242b63820e3f852989adc02af81",
		    "state": "uploaded",
		    "browser_download_url": "https://example.invalid/collectab.zip",
		    "url": "https://api.example.invalid/assets/1",
		    "content_type": "application/zip"
		  }]
		}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(srv.URL, "", srv.Client())
	rel, asset, err := c.Resolve(context.Background(), Query{Owner: "tiamed", Repo: "collectab", Channel: ChannelStable})
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v0.2.18" || asset.Name != "collectab-0.2.18-chrome.zip" {
		t.Fatalf("%s %s", rel.Tag, asset.Name)
	}
	if asset.Digest == "" || asset.Size != 1637982 {
		t.Fatalf("%+v", asset)
	}
	if sawAuth != "" {
		t.Fatalf("auth header on anonymous request: %s", sawAuth)
	}
}

func TestResolveAuthAndRedactedRateLimit(t *testing.T) {
	const token = "ghp_SUPERSECRETTOKENVALUE"
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/a/b/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Fatalf("auth %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message": "API rate limit exceeded for token " + token,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(srv.URL, token, srv.Client())
	_, _, err := c.Resolve(context.Background(), Query{Owner: "a", Repo: "b"})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "SUPERSECRET") {
		t.Fatalf("token leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "rate limit") || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("error: %v", err)
	}
}

func TestDownloadVerifiesDigestAndStripsRedirectAuth(t *testing.T) {
	payload := []byte("zip-bytes-not-a-real-zip")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("authorization forwarded to CDN: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write(payload)
	}))
	defer cdn.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Fatal("expected auth on API host")
		}
		http.Redirect(w, r, cdn.URL+"/file", http.StatusFound)
	}))
	defer api.Close()

	c := NewClient(api.URL, "local-test-token", api.Client())
	dir := t.TempDir()
	dest := dir + "/ext.zip"
	meta, err := c.DownloadAsset(context.Background(), Asset{
		Name:               "ext.zip",
		Size:               int64(len(payload)),
		Digest:             "sha256:" + digest,
		BrowserDownloadURL: api.URL + "/browser",
		APIURL:             api.URL + "/assets/1",
	}, dest, "")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Digest != "sha256:"+digest || meta.Size != int64(len(payload)) || meta.ETag != `"v1"` {
		t.Fatalf("%+v", meta)
	}
	b, err := os.ReadFile(dest)
	if err != nil || string(b) != string(payload) {
		t.Fatalf("file %q err %v", b, err)
	}

	_, err = c.DownloadAsset(context.Background(), Asset{
		Name: "ext.zip", Size: int64(len(payload)), Digest: "sha256:deadbeef",
		BrowserDownloadURL: cdn.URL, APIURL: "",
	}, dest+"2", "")
	// token is set but APIURL empty, so browser URL is used
	if xerr.ExitCode(err) != 4 {
		t.Fatalf("expected verify error, got %v", err)
	}
}

func TestDownloadSizeMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("abc"))
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "", srv.Client())
	_, err := c.DownloadAsset(context.Background(), Asset{
		Name: "a.zip", Size: 99, BrowserDownloadURL: srv.URL + "/a.zip",
	}, t.TempDir()+"/a.zip", "")
	if xerr.ExitCode(err) != 4 {
		t.Fatal(err)
	}
}

func TestNotModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `"abc"` {
			t.Fatalf("etag header %q", r.Header.Get("If-None-Match"))
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "", srv.Client())
	meta, err := c.DownloadAsset(context.Background(), Asset{
		Name: "a.zip", Size: 3, BrowserDownloadURL: srv.URL + "/a",
	}, t.TempDir()+"/a.zip", `"abc"`)
	if err != nil || !meta.NotModified {
		t.Fatalf("%+v %v", meta, err)
	}
}

func TestParseChecksum(t *testing.T) {
	body := "fbc4558d400cfe852ae045088db2035f442ca242b63820e3f852989adc02af81  collectab-0.2.18-chrome.zip\n"
	s256, _ := parseChecksum(body, "collectab-0.2.18-chrome.zip")
	if !strings.HasPrefix(s256, "fbc4558d") {
		t.Fatal(s256)
	}
	only, _ := parseChecksum("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n", "x.zip")
	if len(only) != 64 {
		t.Fatal(only)
	}
}

func TestPaginationPicksNightly(t *testing.T) {
	page2 := danmaku()[:1] // stable only, older
	page1 := danmaku()[1:] // nightly + preview + draft, newer
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/Mr-Quin/danmaku-anywhere/releases", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_ = json.NewEncoder(w).Encode(toAPI(page2))
			return
		}
		w.Header().Set("Link", `<`+srv.URL+`/repos/Mr-Quin/danmaku-anywhere/releases?page=2>; rel="next"`)
		_ = json.NewEncoder(w).Encode(toAPI(page1))
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(srv.URL, "", srv.Client())
	rel, asset, err := c.Resolve(context.Background(), Query{
		Owner: "Mr-Quin", Repo: "danmaku-anywhere", Channel: ChannelNightly,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "nightly-38196101477" || !strings.HasSuffix(asset.Name, "-chrome.zip") {
		t.Fatalf("%s %s", rel.Tag, asset.Name)
	}
}

func toAPI(rels []Release) []apiRelease {
	var out []apiRelease
	for _, rel := range rels {
		raw := apiRelease{
			TagName: rel.Tag, Name: rel.Name, Draft: rel.Draft, Prerelease: rel.Prerelease,
			HTMLURL: rel.HTMLURL, Body: rel.Body,
		}
		tm := rel.PublishedAt
		raw.PublishedAt = &tm
		for _, a := range rel.Assets {
			raw.Assets = append(raw.Assets, apiAsset{
				ID: a.ID, Name: a.Name, Size: a.Size, Digest: a.Digest, State: a.State,
				BrowserDownloadURL: a.BrowserDownloadURL, URL: a.APIURL, ContentType: a.ContentType,
			})
		}
		out = append(out, raw)
	}
	return out
}
