// Package gh talks to the GitHub Releases API and picks an extension asset.
package gh

import "time"

// Release is a GitHub release reduced to the fields ghx needs.
type Release struct {
	Tag         string
	Name        string
	Draft       bool
	Prerelease  bool
	PublishedAt time.Time
	HTMLURL     string
	Body        string
	Assets      []Asset
}

// When is the publish time, or the zero time when GitHub omitted it.
func (r Release) When() time.Time { return r.PublishedAt }

// Asset is one release file.
type Asset struct {
	ID                 int64
	Name               string
	Size               int64
	Digest             string
	State              string
	BrowserDownloadURL string
	APIURL             string
	ContentType        string
}

// Query describes which release and asset to pick.
type Query struct {
	Owner        string
	Repo         string
	Channel      string
	TagPattern   string
	PinTag       string
	AssetPattern string
}

const (
	ChannelStable     = "stable"
	ChannelPrerelease = "prerelease"
	ChannelNightly    = "nightly"
	ChannelPreview    = "preview"
)
