package gh

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"ghx/internal/xerr"
)

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func danmaku() []Release {
	// Names and flags match GitHub as of 2026-10-12. Preview is newer than nightly.
	// The stable v1.5.0 release is older and is what /releases/latest returns.
	return []Release{
		{
			Tag: "v1.5.0", Name: "v1.5.0", PublishedAt: mustTime("2025-06-01T00:00:00Z"),
			Assets: []Asset{
				{Name: "danmaku-anywhere-1.5.0-firefox.zip", Size: 100, State: "uploaded"},
				{Name: "danmaku-anywhere-1.5.0-chrome.crx", Size: 200, State: "uploaded", Digest: "sha256:aa"},
				{Name: "danmaku-anywhere-1.5.0-chrome.zip", Size: 190, State: "uploaded", Digest: "sha256:bb"},
			},
		},
		{
			Tag: "nightly-38196101477", Name: "Preview Build nightly-38196101477 (1670)",
			Prerelease: true, PublishedAt: mustTime("2026-10-11T00:00:00Z"),
			Assets: []Asset{
				{Name: "danmaku-anywhere-1.5.0.1670-firefox.zip", Size: 27044400, State: "uploaded"},
				{Name: "danmaku-anywhere-1.5.0.1670-chrome.crx", Size: 26533253, State: "uploaded", Digest: "sha256:cc"},
				{Name: "danmaku-anywhere-1.5.0.1670-chrome.zip", Size: 26532660, State: "uploaded", Digest: "sha256:dd"},
			},
		},
		{
			Tag: "preview-pr-582", Name: "Preview Build preview-pr-582 (1672)",
			Prerelease: true, PublishedAt: mustTime("2026-10-12T04:41:27Z"),
			Assets: []Asset{
				{Name: "danmaku-anywhere-1.5.0.1672-chrome.crx", Size: 27375188, State: "uploaded"},
				{Name: "danmaku-anywhere-1.5.0.1672-firefox.zip", Size: 28093447, State: "uploaded"},
				{Name: "danmaku-anywhere-1.5.0.1672-chrome.zip", Size: 27374595, State: "uploaded", Digest: "sha256:ae11"},
			},
		},
		{
			Tag: "draft-9", Draft: true, Prerelease: true, PublishedAt: mustTime("2026-10-13T00:00:00Z"),
			Assets: []Asset{{Name: "draft-chrome.zip", Size: 10, State: "uploaded"}},
		},
	}
}

func TestChooseDanmakuStable(t *testing.T) {
	rel, asset, err := Choose(danmaku(), Query{Owner: "Mr-Quin", Repo: "danmaku-anywhere", Channel: ChannelStable}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v1.5.0" || asset.Name != "danmaku-anywhere-1.5.0-chrome.zip" {
		t.Fatalf("got %s %s", rel.Tag, asset.Name)
	}
}

func TestChooseDanmakuPrereleaseIsPreview(t *testing.T) {
	rel, asset, err := Choose(danmaku(), Query{Owner: "Mr-Quin", Repo: "danmaku-anywhere", Channel: ChannelPrerelease}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "preview-pr-582" || asset.Name != "danmaku-anywhere-1.5.0.1672-chrome.zip" {
		t.Fatalf("got %s %s", rel.Tag, asset.Name)
	}
}

func TestChooseNightlyNotPreviewTitle(t *testing.T) {
	rel, asset, err := Choose(danmaku(), Query{Owner: "Mr-Quin", Repo: "danmaku-anywhere", Channel: ChannelNightly}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "nightly-38196101477" || !strings.HasSuffix(asset.Name, "-chrome.zip") {
		t.Fatalf("got %s %s", rel.Tag, asset.Name)
	}
}

func TestChoosePreviewTagOnly(t *testing.T) {
	rel, asset, err := Choose(danmaku(), Query{Owner: "Mr-Quin", Repo: "danmaku-anywhere", Channel: ChannelPreview}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "preview-pr-582" || asset.Name != "danmaku-anywhere-1.5.0.1672-chrome.zip" {
		t.Fatalf("got %s %s", rel.Tag, asset.Name)
	}
}

func TestChooseCollectab(t *testing.T) {
	rels := []Release{{
		Tag: "v0.2.18", PublishedAt: mustTime("2026-08-27T11:01:21Z"),
		Assets: []Asset{{
			Name: "collectab-0.2.18-chrome.zip", Size: 1637982, State: "uploaded",
			Digest: "sha256:fbc4558d400cfe852ae045088db2035f442ca242b63820e3f852989adc02af81",
		}},
	}}
	rel, asset, err := Choose(rels, Query{Owner: "tiamed", Repo: "collectab"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v0.2.18" || asset.Name != "collectab-0.2.18-chrome.zip" {
		t.Fatalf("%s %s", rel.Tag, asset.Name)
	}
}

func TestChooseAssetPattern(t *testing.T) {
	_, asset, err := Choose(danmaku(), Query{
		Owner: "Mr-Quin", Repo: "danmaku-anywhere", Channel: ChannelStable,
		AssetPattern: `chrome\.crx$`,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Name != "danmaku-anywhere-1.5.0-chrome.crx" {
		t.Fatal(asset.Name)
	}
}

func TestChooseExplicitFirefox(t *testing.T) {
	_, asset, err := Choose(danmaku(), Query{
		Owner: "Mr-Quin", Repo: "danmaku-anywhere", Channel: ChannelStable,
		AssetPattern: `firefox\.zip$`,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asset.Name, "firefox") {
		t.Fatal(asset.Name)
	}
}

func TestChooseZipPatternStillPrefersChrome(t *testing.T) {
	_, asset, err := Choose(danmaku(), Query{
		Owner: "Mr-Quin", Repo: "danmaku-anywhere", Channel: ChannelNightly,
		AssetPattern: `\.zip$`,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(asset.Name, "-chrome.zip") {
		t.Fatal(asset.Name)
	}
}

func TestChooseAmbiguous(t *testing.T) {
	rels := []Release{{
		Tag: "v1", PublishedAt: mustTime("2026-01-01T00:00:00Z"),
		Assets: []Asset{
			{Name: "ext-chrome-stable.zip", Size: 10, State: "uploaded"},
			{Name: "ext-chrome-beta.zip", Size: 10, State: "uploaded"},
		},
	}}
	_, _, err := Choose(rels, Query{Owner: "a", Repo: "b"}, nil)
	if err == nil || !strings.Contains(err.Error(), "ext-chrome-beta.zip") {
		t.Fatal(err)
	}
	if xerr.ExitCode(err) != 2 {
		t.Fatalf("exit %d", xerr.ExitCode(err))
	}
}

func TestChooseOnlyFirefox(t *testing.T) {
	rels := []Release{{
		Tag: "v1", PublishedAt: mustTime("2026-01-01T00:00:00Z"),
		Assets: []Asset{{Name: "ext-firefox.zip", Size: 10, State: "uploaded"}},
	}}
	_, _, err := Choose(rels, Query{Owner: "a", Repo: "b"}, nil)
	if xerr.ExitCode(err) != 3 {
		t.Fatal(err)
	}
}

func TestChoosePinPrerelease(t *testing.T) {
	rel, _, err := Choose(danmaku(), Query{Owner: "Mr-Quin", Repo: "danmaku-anywhere", Channel: ChannelStable, PinTag: "nightly-38196101477"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "nightly-38196101477" {
		t.Fatal(rel.Tag)
	}
}

func TestChooseTagPattern(t *testing.T) {
	rel, _, err := Choose(danmaku(), Query{
		Owner: "Mr-Quin", Repo: "danmaku-anywhere", Channel: ChannelPrerelease,
		TagPattern: `^nightly-`,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "nightly-38196101477" {
		t.Fatal(rel.Tag)
	}
}

func TestVersionedPattern(t *testing.T) {
	got := VersionedPattern("danmaku-anywhere-1.5.0.1672-chrome.zip")
	want := `^danmaku-anywhere-\d+\.\d+\.\d+\.\d+-chrome\.zip$`
	if got != want {
		t.Fatalf("%s", got)
	}
	re := regexp.MustCompile(got)
	if !re.MatchString("danmaku-anywhere-1.5.0.1800-chrome.zip") {
		t.Fatal("next build should match")
	}
	if re.MatchString("danmaku-anywhere-1.5.0.1800-firefox.zip") {
		t.Fatal("firefox should not match")
	}
}

func TestSiblingCRX(t *testing.T) {
	rels := danmaku()
	rel := rels[2]
	selected, err := SelectAsset(rel, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	crx, ok := SiblingCRX(rel, selected)
	if !ok || crx.Name != "danmaku-anywhere-1.5.0.1672-chrome.crx" {
		t.Fatalf("%v %+v", ok, crx)
	}
}

func TestIgnoresUnsortedInput(t *testing.T) {
	rels := danmaku()
	// Newest preview is last in this rotated slice.
	rels = append(rels[1:], rels[0])
	rel, _, err := Choose(rels, Query{Owner: "Mr-Quin", Repo: "danmaku-anywhere", Channel: ChannelPrerelease}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "preview-pr-582" {
		t.Fatal(rel.Tag)
	}
}
