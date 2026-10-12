package gh

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"ghx/internal/xerr"
)

// autoExclude drops packages that are not Chrome/Chromium builds when the user
// did not pass an asset regex. Firefox builds from these repos use the same
// zip extension, so the extension alone is not enough.
var autoExclude = regexp.MustCompile(`(?i)(firefox|gecko|thunderbird|safari|android|iphone|\bios\b)`)

var (
	sourcePenalty = regexp.MustCompile(`(?i)(^|[._-])(source|src)([._-]|$)`)
	osPenalty     = regexp.MustCompile(`(?i)(^|[._-])(linux|windows|darwin|macos|win32|win64|amd64|arm64|x86_64)([._-]|$)`)
	nightlyRE     = regexp.MustCompile(`(?i)nightly`)
	previewTagRE  = regexp.MustCompile(`(?i)preview`)
)

// Allows reports whether rel is a candidate for q.
//
// Preview matching uses the tag only. Some projects title every nightly
// "Preview Build nightly-…", so a name search for "preview" would swallow nightlies.
func Allows(rel Release, q Query) bool {
	if rel.Draft {
		return false
	}
	if q.PinTag != "" {
		if rel.Tag != q.PinTag {
			return false
		}
	} else {
		channel := q.Channel
		if channel == "" {
			channel = ChannelStable
		}
		switch channel {
		case ChannelStable:
			if rel.Prerelease {
				return false
			}
		case ChannelPrerelease:
		case ChannelNightly:
			if !rel.Prerelease && !nightlyRE.MatchString(rel.Tag) {
				return false
			}
			if !nightlyRE.MatchString(rel.Tag) && !nightlyRE.MatchString(rel.Name) {
				return false
			}
		case ChannelPreview:
			if !previewTagRE.MatchString(rel.Tag) {
				return false
			}
		default:
			return false
		}
	}
	if q.TagPattern != "" {
		re, err := regexp.Compile(q.TagPattern)
		if err != nil || !re.MatchString(rel.Tag) {
			return false
		}
	}
	return true
}

// Choose picks the newest allowed release and its best asset.
// releases may be in any order.
func Choose(releases []Release, q Query, logf func(string, ...any)) (Release, Asset, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if q.Channel != "" && q.Channel != ChannelStable && q.Channel != ChannelPrerelease && q.Channel != ChannelNightly && q.Channel != ChannelPreview {
		return Release{}, Asset{}, fmt.Errorf("channel must be stable, prerelease, nightly, or preview")
	}
	if q.AssetPattern != "" {
		if _, err := regexp.Compile(q.AssetPattern); err != nil {
			return Release{}, Asset{}, fmt.Errorf("asset pattern: %w", err)
		}
	}
	if q.TagPattern != "" {
		if _, err := regexp.Compile(q.TagPattern); err != nil {
			return Release{}, Asset{}, fmt.Errorf("tag pattern: %w", err)
		}
	}
	sorted := append([]Release(nil), releases...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].When().After(sorted[j].When())
	})
	for _, rel := range sorted {
		if !Allows(rel, q) {
			logf("skip %s draft=%v prerelease=%v (channel %s)", rel.Tag, rel.Draft, rel.Prerelease, emptyChannel(q.Channel))
			continue
		}
		asset, err := SelectAsset(rel, q.AssetPattern, logf)
		if err != nil {
			if isNoAsset(err) {
				logf("skip %s: %v", rel.Tag, err)
				continue
			}
			return Release{}, Asset{}, err
		}
		logf("selected %s asset %s (%d bytes, digest %s)", rel.Tag, asset.Name, asset.Size, asset.Digest)
		return rel, asset, nil
	}
	return Release{}, Asset{}, fmt.Errorf("%w: no %s release of %s/%s has a matching Chrome package", xerr.ErrNotFound, emptyChannel(q.Channel), q.Owner, q.Repo)
}

func emptyChannel(c string) string {
	if c == "" {
		return ChannelStable
	}
	return c
}

func isNoAsset(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "no asset")
}

// SelectAsset picks one package from rel.
// Without a pattern, firefox/safari/android packages are ignored and
// chrome zip outranks chrome crx, which outranks a generic zip.
func SelectAsset(rel Release, pattern string, logf func(string, ...any)) (Asset, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	var re *regexp.Regexp
	if pattern != "" {
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			return Asset{}, fmt.Errorf("asset pattern: %w", err)
		}
	}
	type scored struct {
		asset Asset
		score int
	}
	var all []scored
	for _, a := range rel.Assets {
		if a.Name == "" || (a.State != "" && a.State != "uploaded") || a.Size <= 0 {
			continue
		}
		if !packageExt(a.Name) {
			continue
		}
		if re != nil && !re.MatchString(a.Name) {
			continue
		}
		if re == nil && autoExclude.MatchString(a.Name) {
			logf("exclude %s (not a Chrome package)", a.Name)
			continue
		}
		s := scoreAsset(a.Name)
		logf("candidate %s score %d", a.Name, s)
		all = append(all, scored{asset: a, score: s})
	}
	if len(all) == 0 {
		if pattern != "" {
			return Asset{}, fmt.Errorf("no asset in %s matches %s", rel.Tag, pattern)
		}
		return Asset{}, fmt.Errorf("no asset in %s looks like a Chrome extension (.zip or .crx)", rel.Tag)
	}
	best := all[0].score
	for _, c := range all[1:] {
		if c.score > best {
			best = c.score
		}
	}
	var winners []Asset
	for _, c := range all {
		if c.score == best {
			winners = append(winners, c.asset)
		}
	}
	if len(winners) == 1 {
		return winners[0], nil
	}
	sort.Slice(winners, func(i, j int) bool { return winners[i].Name < winners[j].Name })
	return Asset{}, &AmbiguousError{Release: rel, Assets: winners, Score: best}
}

// AmbiguousError is returned when asset scoring does not produce a single winner.
type AmbiguousError struct {
	Release Release
	Assets  []Asset
	Score   int
}

func (e *AmbiguousError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "release %s has %d assets with score %d:", e.Release.Tag, len(e.Assets), e.Score)
	for i, w := range e.Assets {
		fmt.Fprintf(&b, "\n  %d) %s (%d bytes)", i+1, w.Name, w.Size)
	}
	b.WriteString("\nPass --asset with a regex that matches one file, for example chrome\\.zip$")
	return b.String()
}

func (e *AmbiguousError) Unwrap() error { return xerr.ErrAmbiguous }

// VersionedPattern turns a concrete asset name into a regex that still matches
// later releases whose version numbers changed.
func VersionedPattern(name string) string {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(name); {
		if name[i] >= '0' && name[i] <= '9' {
			for i < len(name) && name[i] >= '0' && name[i] <= '9' {
				i++
			}
			b.WriteString(`\d+`)
			continue
		}
		b.WriteString(regexp.QuoteMeta(name[i : i+1]))
		i++
	}
	b.WriteByte('$')
	return b.String()
}

// SelectCRX picks the Chrome CRX on rel, if one exists.
func SelectCRX(rel Release, logf func(string, ...any)) (Asset, error) {
	return SelectAsset(rel, `(?i)\.crx$`, logf)
}

func packageExt(name string) bool {
	low := strings.ToLower(name)
	return strings.HasSuffix(low, ".zip") || strings.HasSuffix(low, ".crx") || strings.HasSuffix(low, ".crx3")
}

func scoreAsset(name string) int {
	low := strings.ToLower(name)
	s := 0
	switch {
	case strings.HasSuffix(low, ".zip"):
		s += 50
	case strings.HasSuffix(low, ".crx"), strings.HasSuffix(low, ".crx3"):
		s += 30
	}
	if strings.Contains(low, "chrome") {
		s += 80
	}
	if strings.Contains(low, "chromium") {
		s += 70
	}
	if strings.Contains(low, "extension") {
		s += 40
	}
	if strings.Contains(low, "unpacked") {
		s += 10
	}
	if sourcePenalty.MatchString(name) {
		s -= 60
	}
	if osPenalty.MatchString(name) {
		s -= 25
	}
	return s
}

// SiblingCRX returns the CRX that pairs with a zip asset, if the release has one.
func SiblingCRX(rel Release, selected Asset) (Asset, bool) {
	low := strings.ToLower(selected.Name)
	if strings.HasSuffix(low, ".crx") || strings.HasSuffix(low, ".crx3") {
		return selected, true
	}
	base := selected.Name
	if i := strings.LastIndex(base, "."); i >= 0 {
		base = base[:i]
	}
	want := base + ".crx"
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, want) && a.Size > 0 {
			return a, true
		}
	}
	asset, err := SelectCRX(rel, nil)
	if err != nil {
		return Asset{}, false
	}
	return asset, true
}
