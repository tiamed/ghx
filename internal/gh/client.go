package gh

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ghx/internal/redact"
	"ghx/internal/version"
	"ghx/internal/xerr"
)

const (
	maxAssetBytes = 512 << 20
	maxPages      = 10
	pageSize      = 100
)

// Client is a GitHub Releases API client.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
	Logf  func(string, ...any)
}

// NewClient builds a client. token may be empty.
func NewClient(base, token string, httpClient *http.Client) *Client {
	if base == "" {
		base = "https://api.github.com"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Minute}
	}
	return &Client{Base: strings.TrimRight(base, "/"), Token: token, HTTP: httpClient}
}

func (c *Client) logf(format string, args ...any) {
	if c.Logf == nil {
		return
	}
	c.Logf(format, args...)
}

// Resolve finds the release and asset for q.
func (c *Client) Resolve(ctx context.Context, q Query) (Release, Asset, error) {
	if q.PinTag != "" {
		rel, err := c.ReleaseByTag(ctx, q.Owner, q.Repo, q.PinTag)
		if err != nil {
			return Release{}, Asset{}, err
		}
		asset, err := SelectAsset(rel, q.AssetPattern, c.Logf)
		if err != nil {
			return Release{}, Asset{}, err
		}
		return rel, asset, nil
	}
	channel := q.Channel
	if channel == "" {
		channel = ChannelStable
	}
	if channel == ChannelStable && q.TagPattern == "" {
		rel, err := c.Latest(ctx, q.Owner, q.Repo)
		if err == nil {
			asset, aerr := SelectAsset(rel, q.AssetPattern, c.Logf)
			if aerr == nil || errors.Is(aerr, xerr.ErrAmbiguous) {
				return rel, asset, aerr
			}
			c.logf("latest stable %s has no matching asset (%v); searching older stable releases", rel.Tag, aerr)
		} else if !errors.Is(err, xerr.ErrNotFound) {
			return Release{}, Asset{}, err
		} else {
			return Release{}, Asset{}, fmt.Errorf("%w: %s/%s has no stable release (GitHub /releases/latest returned 404). Pass --channel prerelease or --channel nightly", xerr.ErrNotFound, q.Owner, q.Repo)
		}
	}

	var acc []Release
	var chosen Release
	var chosenAsset Asset
	found := false
	err := c.eachPage(ctx, q.Owner, q.Repo, func(page []Release) (bool, error) {
		acc = append(acc, page...)
		rel, asset, err := Choose(acc, q, c.Logf)
		if errors.Is(err, xerr.ErrAmbiguous) {
			return true, err
		}
		if err != nil && !errors.Is(err, xerr.ErrNotFound) {
			return true, err
		}
		if err == nil {
			chosen, chosenAsset, found = rel, asset, true
			if len(page) == 0 || !page[len(page)-1].When().After(rel.When()) {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return Release{}, Asset{}, err
	}
	if !found {
		rel, asset, err := Choose(acc, q, c.Logf)
		return rel, asset, err
	}
	return chosen, chosenAsset, nil
}

// Latest is GET /repos/{owner}/{repo}/releases/latest (newest non-prerelease).
func (c *Client) Latest(ctx context.Context, owner, repo string) (Release, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.Base, owner, repo)
	body, status, _, err := c.get(ctx, u, "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	if status == http.StatusNotFound {
		return Release{}, fmt.Errorf("%w: no stable release", xerr.ErrNotFound)
	}
	if status < 200 || status >= 300 {
		return Release{}, httpError(status, body, c.Token)
	}
	return decodeRelease(body)
}

// ReleaseByTag loads one tag's release.
func (c *Client) ReleaseByTag(ctx context.Context, owner, repo, tag string) (Release, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", c.Base, owner, repo, tag)
	body, status, _, err := c.get(ctx, u, "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	if status == http.StatusNotFound {
		return Release{}, fmt.Errorf("%w: no release tagged %s", xerr.ErrNotFound, tag)
	}
	if status < 200 || status >= 300 {
		return Release{}, httpError(status, body, c.Token)
	}
	return decodeRelease(body)
}

func (c *Client) eachPage(ctx context.Context, owner, repo string, fn func([]Release) (bool, error)) error {
	u := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=%d", c.Base, owner, repo, pageSize)
	for page := 1; page <= maxPages && u != ""; page++ {
		body, status, hdr, err := c.get(ctx, u, "application/vnd.github+json")
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return httpError(status, body, c.Token)
		}
		rels, err := decodeReleaseList(body)
		if err != nil {
			return err
		}
		stop, err := fn(rels)
		if err != nil || stop {
			return err
		}
		u = nextLink(hdr.Get("Link"))
	}
	return nil
}

type apiRelease struct {
	TagName     string     `json:"tag_name"`
	Name        string     `json:"name"`
	Draft       bool       `json:"draft"`
	Prerelease  bool       `json:"prerelease"`
	PublishedAt *time.Time `json:"published_at"`
	HTMLURL     string     `json:"html_url"`
	Body        string     `json:"body"`
	Assets      []apiAsset `json:"assets"`
}

type apiAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
	State              string `json:"state"`
	BrowserDownloadURL string `json:"browser_download_url"`
	URL                string `json:"url"`
	ContentType        string `json:"content_type"`
}

func decodeRelease(b []byte) (Release, error) {
	var raw apiRelease
	if err := json.Unmarshal(b, &raw); err != nil {
		return Release{}, fmt.Errorf("decode release: %w", err)
	}
	return convertRelease(raw), nil
}

func decodeReleaseList(b []byte) ([]Release, error) {
	var raw []apiRelease
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("decode releases: %w", err)
	}
	out := make([]Release, 0, len(raw))
	for _, r := range raw {
		out = append(out, convertRelease(r))
	}
	return out, nil
}

func convertRelease(raw apiRelease) Release {
	rel := Release{
		Tag:        raw.TagName,
		Name:       raw.Name,
		Draft:      raw.Draft,
		Prerelease: raw.Prerelease,
		HTMLURL:    raw.HTMLURL,
		Body:       raw.Body,
	}
	if raw.PublishedAt != nil {
		rel.PublishedAt = *raw.PublishedAt
	}
	for _, a := range raw.Assets {
		rel.Assets = append(rel.Assets, Asset{
			ID:                 a.ID,
			Name:               a.Name,
			Size:               a.Size,
			Digest:             a.Digest,
			State:              a.State,
			BrowserDownloadURL: a.BrowserDownloadURL,
			APIURL:             a.URL,
			ContentType:        a.ContentType,
		})
	}
	return rel
}

// DownloadMeta is the verified result of an asset download.
type DownloadMeta struct {
	NotModified bool
	Path        string
	Size        int64
	Digest      string // sha256:<hex> computed locally
	ETag        string
}

// DownloadAsset streams asset to dest and verifies size plus any digest.
// If ifNoneMatch is set and the server returns 304, NotModified is true and dest is left untouched.
func (c *Client) DownloadAsset(ctx context.Context, asset Asset, dest, ifNoneMatch string) (DownloadMeta, error) {
	if asset.Size > maxAssetBytes {
		return DownloadMeta{}, fmt.Errorf("%w: %s is %d bytes, over the %d byte limit", xerr.ErrVerify, asset.Name, asset.Size, maxAssetBytes)
	}
	url, accept, auth := asset.BrowserDownloadURL, "", false
	if c.Token != "" && asset.APIURL != "" {
		url, accept, auth = asset.APIURL, "application/octet-stream", true
	}
	if url == "" {
		return DownloadMeta{}, fmt.Errorf("%w: asset %s has no download URL", xerr.ErrNotFound, asset.Name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return DownloadMeta{}, err
	}
	c.decorate(req, accept, auth)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	c.logf("GET %s authorization=%s", redact.Clean(req.URL.String(), c.Token), present(req.Header.Get("Authorization")))
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return DownloadMeta{}, fmt.Errorf("download %s: %w", asset.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		c.logf("download %s: not modified", asset.Name)
		return DownloadMeta{NotModified: true, ETag: ifNoneMatch}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return DownloadMeta{}, httpError(resp.StatusCode, b, c.Token)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return DownloadMeta{}, err
	}
	tmp := dest + ".partial"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return DownloadMeta{}, err
	}
	sha := sha256.New()
	sha5 := sha512.New()
	n, copyErr := io.Copy(io.MultiWriter(f, sha, sha5), io.LimitReader(resp.Body, maxAssetBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return DownloadMeta{}, copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return DownloadMeta{}, closeErr
	}
	if n > maxAssetBytes {
		os.Remove(tmp)
		return DownloadMeta{}, fmt.Errorf("%w: %s exceeded %d bytes", xerr.ErrVerify, asset.Name, maxAssetBytes)
	}
	if asset.Size > 0 && n != asset.Size {
		os.Remove(tmp)
		return DownloadMeta{}, fmt.Errorf("%w: %s size is %d, GitHub reported %d", xerr.ErrVerify, asset.Name, n, asset.Size)
	}
	sum256 := hex.EncodeToString(sha.Sum(nil))
	sum512 := hex.EncodeToString(sha5.Sum(nil))
	if err := matchDigest(asset.Name, asset.Digest, sum256, sum512); err != nil {
		os.Remove(tmp)
		return DownloadMeta{}, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return DownloadMeta{}, err
	}
	etag := resp.Header.Get("ETag")
	return DownloadMeta{Path: dest, Size: n, Digest: "sha256:" + sum256, ETag: etag}, nil
}

func matchDigest(name, digest, sum256, sum512 string) error {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil
	}
	algo, hexsum, ok := strings.Cut(digest, ":")
	if !ok {
		return fmt.Errorf("%w: %s digest %q is not algo:hex", xerr.ErrVerify, name, digest)
	}
	hexsum = strings.ToLower(strings.TrimSpace(hexsum))
	switch strings.ToLower(algo) {
	case "sha256":
		if hexsum != sum256 {
			return fmt.Errorf("%w: %s sha256 is %s, GitHub digest is %s", xerr.ErrVerify, name, sum256, hexsum)
		}
	case "sha512":
		if hexsum != sum512 {
			return fmt.Errorf("%w: %s sha512 does not match the GitHub digest", xerr.ErrVerify, name)
		}
	default:
		return fmt.Errorf("%w: %s digest algorithm %s is not supported", xerr.ErrVerify, name, algo)
	}
	return nil
}

// ChecksumFor looks for a sibling checksum asset and returns hex digests for assetName.
func (c *Client) ChecksumFor(ctx context.Context, rel Release, assetName string) (sha256hex, sha512hex string, err error) {
	var files []Asset
	for _, a := range rel.Assets {
		low := strings.ToLower(a.Name)
		if low == strings.ToLower(assetName)+".sha256" || low == strings.ToLower(assetName)+".sha256sum" ||
			low == "sha256sums" || low == "sha256sums.txt" || low == "checksums.txt" ||
			low == strings.ToLower(assetName)+".sha512" {
			if a.Size > 0 && a.Size < 1<<20 {
				files = append(files, a)
			}
		}
	}
	for _, a := range files {
		dir, err := os.MkdirTemp("", "ghx-sum-")
		if err != nil {
			return "", "", err
		}
		dest := filepath.Join(dir, sanitize(a.Name))
		_, err = c.DownloadAsset(ctx, Asset{
			Name: a.Name, Size: a.Size, Digest: a.Digest,
			BrowserDownloadURL: a.BrowserDownloadURL, APIURL: a.APIURL,
		}, dest, "")
		if err != nil {
			os.RemoveAll(dir)
			return "", "", err
		}
		b, err := os.ReadFile(dest)
		os.RemoveAll(dir)
		if err != nil {
			return "", "", err
		}
		s256, s512 := parseChecksum(string(b), assetName)
		if sha256hex == "" {
			sha256hex = s256
		}
		if sha512hex == "" {
			sha512hex = s512
		}
	}
	return sha256hex, sha512hex, nil
}

func sanitize(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, name)
}

// parseChecksum extracts a hex digest for filename from a checksum file.
func parseChecksum(body, filename string) (sha256hex, sha512hex string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", ""
	}
	lines := strings.Split(body, "\n")
	if len(lines) == 1 && isHex(strings.TrimSpace(lines[0])) {
		h := strings.ToLower(strings.TrimSpace(lines[0]))
		switch len(h) {
		case 64:
			return h, ""
		case 128:
			return "", h
		}
	}
	base := filepath.Base(filename)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		h := strings.ToLower(fields[0])
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name != filename && name != base && filepath.Base(name) != base {
			continue
		}
		if len(h) == 64 && isHex(h) {
			sha256hex = h
		} else if len(h) == 128 && isHex(h) {
			sha512hex = h
		}
	}
	return sha256hex, sha512hex
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func (c *Client) get(ctx context.Context, u, accept string) ([]byte, int, http.Header, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, nil, err
	}
	c.decorate(req, accept, c.Token != "")
	c.logf("GET %s authorization=%s", redact.Clean(req.URL.String(), c.Token), present(req.Header.Get("Authorization")))
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("github request: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, resp.Header, err
	}
	c.logf("-> %d %s", resp.StatusCode, redact.Clean(u, c.Token))
	return b, resp.StatusCode, resp.Header, nil
}

func (c *Client) decorate(req *http.Request, accept string, auth bool) {
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "ghx/"+version.Version)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if auth && c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}

// httpClient strips Authorization on redirects. GitHub asset downloads redirect
// to a file host that must not receive the token.
func (c *Client) httpClient() *http.Client {
	base := c.HTTP
	if base == nil {
		base = &http.Client{Timeout: 10 * time.Minute}
	}
	clone := *base
	prev := clone.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 {
			req.Header.Del("Authorization")
		}
		if prev != nil {
			return prev(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &clone
}

func present(v string) string {
	if v == "" {
		return "absent"
	}
	return "present"
}

func httpError(status int, body []byte, token string) error {
	msg := strings.TrimSpace(string(body))
	if len(msg) > 300 {
		msg = msg[:300]
	}
	var parsed struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Message != "" {
		msg = parsed.Message
	}
	msg = redact.Clean(msg, token)
	if status == http.StatusForbidden || status == http.StatusTooManyRequests || status == http.StatusUnauthorized {
		low := strings.ToLower(msg)
		if strings.Contains(low, "rate limit") || status == http.StatusTooManyRequests {
			return fmt.Errorf("GitHub rate limit (HTTP %d): %s. Set GITHUB_TOKEN or GH_TOKEN for a higher limit", status, msg)
		}
	}
	if status == http.StatusNotFound {
		return fmt.Errorf("%w: GitHub HTTP 404: %s", xerr.ErrNotFound, msg)
	}
	return fmt.Errorf("GitHub HTTP %d: %s", status, msg)
}

func nextLink(h string) string {
	for _, part := range strings.Split(h, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		i := strings.Index(part, "<")
		j := strings.Index(part, ">")
		if i >= 0 && j > i {
			return part[i+1 : j]
		}
	}
	return ""
}

// CompareDigest reports whether remote matches a stored sha256 or the original API digest string.
func CompareDigest(remote, storedSHA, storedRemote string) bool {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return false
	}
	if storedRemote != "" && strings.EqualFold(remote, storedRemote) {
		return true
	}
	if storedSHA == "" {
		return false
	}
	algo, hexsum, ok := strings.Cut(strings.ToLower(remote), ":")
	if !ok || algo != "sha256" {
		return false
	}
	_, local, ok := strings.Cut(strings.ToLower(storedSHA), ":")
	if !ok {
		local = strings.ToLower(storedSHA)
	}
	return hexsum == local
}

// CacheName returns a single path segment for an asset file name.
func CacheName(name string) (string, error) {
	base := filepath.Base(name)
	if base != name || base == "." || base == string(filepath.Separator) || strings.ContainsAny(base, `/\`) {
		return "", fmt.Errorf("%w: asset name %q is not a single path segment", xerr.ErrVerify, name)
	}
	return base, nil
}
