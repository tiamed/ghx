package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"ghx/internal/archive"
	"ghx/internal/browser"
	"ghx/internal/config"
	"ghx/internal/gh"
	"ghx/internal/install"
	"ghx/internal/state"
	"ghx/internal/xerr"
)

func (a *app) add(args []string) int {
	bools := map[string]bool{"force": true, "track-only": true, "dry-run": true, "prerelease": true, "json": true}
	args, err := reorder(args, bools)
	if err != nil {
		return a.fail(err)
	}
	fs := a.newFlagSet("add")
	name := fs.String("name", "", "local name (default: repository name)")
	channel := fs.String("channel", "", "stable, prerelease, nightly, or preview")
	tag := fs.String("tag", "", "tag regex")
	asset := fs.String("asset", "", "asset name regex")
	browserName := fs.String("browser", "", "chrome, chromium, edge, brave, or chrome-for-testing")
	pin := fs.String("pin", "", "exact release tag")
	force := fs.Bool("force", false, "replace an existing name")
	trackOnly := fs.Bool("track-only", false, "save the config without downloading")
	dry := fs.Bool("dry-run", false, "resolve the release without downloading")
	pre := fs.Bool("prerelease", false, "same as --channel prerelease")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprint(a.stderr, commandHelp("add"))
		return 1
	}
	owner, repo, urlPin, err := config.ParseRepoRef(fs.Arg(0))
	if err != nil {
		return a.fail(err)
	}
	ch := strings.ToLower(strings.TrimSpace(*channel))
	if *pre {
		if ch != "" && ch != gh.ChannelPrerelease {
			return a.fail(fmt.Errorf("--prerelease conflicts with --channel %s", ch))
		}
		ch = gh.ChannelPrerelease
	}
	if ch == "" {
		ch = gh.ChannelStable
	}
	chosenPin := *pin
	if chosenPin == "" {
		chosenPin = urlPin
	} else if urlPin != "" && urlPin != chosenPin {
		return a.fail(fmt.Errorf("URL tag %s does not match --pin %s", urlPin, chosenPin))
	}
	local := *name
	if local == "" {
		local = repo
	}
	ext := config.Extension{
		Name: local, Repo: owner + "/" + repo, Channel: ch,
		TagPattern: *tag, AssetPattern: *asset, PinTag: chosenPin, Browser: *browserName,
	}
	f, err := config.Load(a.configPath)
	if err != nil {
		return a.fail(err)
	}
	st, err := state.Load(filepath.Join(a.dataDir, "state.json"))
	if err != nil {
		return a.fail(err)
	}
	replaced := false
	wasNew := true
	for i := range f.Extensions {
		if f.Extensions[i].Name == ext.Name {
			wasNew = false
			_, installed := st.Extensions[ext.Name]
			if !*force && installed {
				return a.fail(fmt.Errorf("%s is already tracked (pass --force to replace it)", ext.Name))
			}
			f.Extensions[i] = ext
			replaced = true
		}
	}
	if !replaced {
		f.Extensions = append(f.Extensions, ext)
	}
	if err := config.Save(a.configPath, f); err != nil {
		return a.fail(err)
	}
	if *trackOnly {
		fmt.Fprintf(a.stdout, "tracking %s (%s)\n", ext.Name, ext.Repo)
		return 0
	}
	fmt.Fprintf(a.stderr, "tracking %s (%s)\n", ext.Name, ext.Repo)
	code := a.updateNames([]string{ext.Name}, *dry, false)
	if code != 0 && wasNew && !*dry {
		if rb := a.rollbackAdd(ext.Name); rb != nil {
			fmt.Fprintf(a.stderr, "ghx: also failed to roll back config: %s\n", a.clean(rb.Error()))
		}
	}
	return code
}

func (a *app) rollbackAdd(name string) error {
	f, err := config.Load(a.configPath)
	if err != nil {
		return err
	}
	var out []config.Extension
	for _, e := range f.Extensions {
		if e.Name != name {
			out = append(out, e)
		}
	}
	if out == nil {
		out = []config.Extension{}
	}
	f.Extensions = out
	return config.Save(a.configPath, f)
}

func (a *app) list(args []string) int {
	args, err := reorder(args, map[string]bool{"json": true})
	if err != nil {
		return a.fail(err)
	}
	fs := a.newFlagSet("list")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprint(a.stderr, commandHelp("list"))
		return 1
	}
	f, err := config.Load(a.configPath)
	if err != nil {
		return a.fail(err)
	}
	st, err := state.Load(filepath.Join(a.dataDir, "state.json"))
	if err != nil {
		return a.fail(err)
	}
	if len(f.Extensions) == 0 && !*asJSON {
		fmt.Fprint(a.stdout, `No extensions are tracked.
Add one:
  ghx add https://github.com/owner/my-extension/releases
  ghx add https://github.com/owner/nightly-extension/releases --channel nightly
`)
		return 0
	}
	type row struct {
		Name      string `json:"name"`
		Repo      string `json:"repo"`
		Channel   string `json:"channel"`
		Tag       string `json:"tag,omitempty"`
		Version   string `json:"version,omitempty"`
		Path      string `json:"path,omitempty"`
		Installed bool   `json:"installed"`
	}
	rows := make([]row, 0, len(f.Extensions))
	for _, e := range f.Extensions {
		ch := e.Channel
		if ch == "" {
			ch = "stable"
		}
		item := row{Name: e.Name, Repo: e.Repo, Channel: ch}
		if inst, ok := st.Extensions[e.Name]; ok && inst.Path != "" {
			item.Installed = true
			item.Tag = inst.Tag
			item.Version = inst.Version
			item.Path = inst.Path
		}
		rows = append(rows, item)
	}
	if *asJSON {
		enc := json.NewEncoder(a.stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"extensions": rows}); err != nil {
			return a.fail(err)
		}
		return 0
	}
	w := tabwriter.NewWriter(a.stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tREPO\tCHANNEL\tTAG\tVERSION\tUNPACKED")
	for _, r := range rows {
		tag, ver, path := r.Tag, r.Version, r.Path
		if tag == "" {
			tag = "-"
		}
		if ver == "" {
			ver = "-"
		}
		if path == "" {
			path = "(not installed)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, r.Repo, r.Channel, tag, ver, path)
	}
	w.Flush()
	return 0
}

func (a *app) remove(args []string) int {
	args, err := reorder(args, map[string]bool{"keep-files": true})
	if err != nil {
		return a.fail(err)
	}
	fs := a.newFlagSet("remove")
	keep := fs.Bool("keep-files", false, "leave the unpacked directory on disk")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprint(a.stderr, commandHelp("remove"))
		return 1
	}
	name := fs.Arg(0)
	f, err := config.Load(a.configPath)
	if err != nil {
		return a.fail(err)
	}
	var next []config.Extension
	found := false
	for _, e := range f.Extensions {
		if e.Name == name {
			found = true
			continue
		}
		next = append(next, e)
	}
	if !found {
		return a.fail(fmt.Errorf("%w: %s is not tracked", xerr.ErrNotFound, name))
	}
	f.Extensions = next
	if err := config.Save(a.configPath, f); err != nil {
		return a.fail(err)
	}
	st, err := state.Load(filepath.Join(a.dataDir, "state.json"))
	if err != nil {
		return a.fail(err)
	}
	inst := st.Extensions[name]
	if *keep {
		delete(st.Extensions, name)
		if err := state.Save(filepath.Join(a.dataDir, "state.json"), st); err != nil {
			return a.fail(err)
		}
		fmt.Fprintf(a.stdout, "stopped tracking %s\n", name)
		if inst.Path != "" {
			fmt.Fprintf(a.stdout, "kept files at %s\n", inst.Path)
		}
	} else {
		if err := install.RemoveFiles(a.dataDir, name); err != nil {
			return a.fail(err)
		}
		fmt.Fprintf(a.stdout, "removed %s\n", name)
	}
	fmt.Fprintln(a.stdout, "If Chrome already loaded it, remove the card at chrome://extensions.")
	return 0
}

func (a *app) update(args []string, checkOnly bool) int {
	bools := map[string]bool{"dry-run": true, "json": true}
	args, err := reorder(args, bools)
	if err != nil {
		return a.fail(err)
	}
	name := "update"
	if checkOnly {
		name = "check"
	}
	fs := a.newFlagSet(name)
	dry := fs.Bool("dry-run", false, "show what would change")
	asJSON := fs.Bool("json", false, "print JSON")
	if checkOnly {
		*dry = true
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() > 1 {
		fmt.Fprint(a.stderr, commandHelp(name))
		return 1
	}
	var names []string
	if fs.NArg() == 1 {
		names = []string{fs.Arg(0)}
	}
	return a.updateNames(names, *dry || checkOnly, *asJSON)
}

func (a *app) updateNames(names []string, dry, asJSON bool) int {
	f, err := config.Load(a.configPath)
	if err != nil {
		return a.fail(err)
	}
	var exts []config.Extension
	if len(names) == 0 {
		exts = f.Extensions
		if len(exts) == 0 {
			fmt.Fprintln(a.stderr, "ghx: no extensions are tracked")
			return 1
		}
	} else {
		for _, n := range names {
			e, ok := f.Find(n)
			if !ok {
				return a.fail(fmt.Errorf("%w: %s is not tracked", xerr.ErrNotFound, n))
			}
			exts = append(exts, e)
		}
	}
	var results []install.Result
	var codes []int
	for _, ext := range exts {
		a.patternName = ext.Name
		ctx := a.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		res, err := install.Apply(ctx, ext, install.Options{
			DataDir: a.dataDir,
			Client:  a.client(),
			DryRun:  dry,
			GOOS:    a.goos,
			Stderr:  a.stderr,
			Pick:    a.pick,
		})
		a.patternName = ""
		if err != nil {
			msg := a.clean(err.Error())
			fmt.Fprintf(a.stderr, "ghx: %s: %s\n", ext.Name, msg)
			results = append(results, install.Result{Name: ext.Name, Repo: ext.Repo, Error: msg})
			codes = append(codes, xerr.ExitCode(err))
			continue
		}
		results = append(results, res)
		codes = append(codes, 0)
		if !asJSON {
			a.printResult(res, dry)
		}
	}
	if asJSON {
		enc := json.NewEncoder(a.stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"results": results}); err != nil {
			return a.fail(err)
		}
	}
	return summarize(codes)
}

func (a *app) printResult(res install.Result, dry bool) {
	switch res.Action {
	case "unchanged":
		fmt.Fprintf(a.stdout, "%s is up to date (%s, tag %s)\n", res.Name, emptyDash(res.Version), res.Tag)
	case "dry-run":
		verb := "install"
		if res.PreviousTag != "" {
			verb = "update " + res.PreviousTag + " -> " + res.Tag
		} else {
			verb = "install " + res.Tag
		}
		fmt.Fprintf(a.stdout, "%s: would %s (%s, %s)\n", res.Name, verb, res.Asset, install.HumanSize(res.Size))
	case "installed":
		fmt.Fprintf(a.stdout, "installed %s %s (tag %s)\n", res.Name, res.Version, res.Tag)
		fmt.Fprintf(a.stdout, "  %s\n", res.Path)
		if res.PathID != "" {
			fmt.Fprintf(a.stdout, "  unpacked extension id: %s\n", res.PathID)
		}
		fmt.Fprintf(a.stdout, "Load unpacked once: chrome://extensions → Developer mode → Load unpacked.\nAfter later updates, click Reload on the card.\n")
	case "updated":
		from := res.PreviousTag
		if from == "" {
			from = "?"
		}
		fmt.Fprintf(a.stdout, "updated %s %s → %s (%s)\n", res.Name, from, res.Tag, res.Version)
		fmt.Fprintf(a.stdout, "  %s\n", res.Path)
		fmt.Fprintln(a.stdout, "Click Reload on the extension card at chrome://extensions.")
	default:
		fmt.Fprintf(a.stdout, "%s: %s\n", res.Name, res.Action)
	}
	if res.Warning != "" {
		fmt.Fprintf(a.stderr, "ghx: %s: %s\n", res.Name, res.Warning)
	}
	_ = dry
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func summarize(codes []int) int {
	if len(codes) == 0 {
		return 0
	}
	failed := 0
	last := 0
	for _, c := range codes {
		if c != 0 {
			failed++
			last = c
		}
	}
	if failed == 0 {
		return 0
	}
	if len(codes) == 1 {
		return last
	}
	return 5
}

func (a *app) path(args []string) int {
	fs := a.newFlagSet("path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprint(a.stderr, commandHelp("path"))
		return 1
	}
	st, err := state.Load(filepath.Join(a.dataDir, "state.json"))
	if err != nil {
		return a.fail(err)
	}
	inst, ok := st.Extensions[fs.Arg(0)]
	if !ok || inst.Path == "" {
		return a.fail(fmt.Errorf("%w: %s is not installed", xerr.ErrNotFound, fs.Arg(0)))
	}
	fmt.Fprintln(a.stdout, inst.Path)
	return 0
}

func (a *app) launch(args []string) int {
	args, err := reorder(args, map[string]bool{"chrome-workaround": true})
	if err != nil {
		return a.fail(err)
	}
	fs := a.newFlagSet("launch")
	browserName := fs.String("browser", "", "browser to start")
	workaround := fs.Bool("chrome-workaround", false, "pass the unsupported branded-Chrome feature flag")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() > 1 {
		fmt.Fprint(a.stderr, commandHelp("launch"))
		return 1
	}
	f, err := config.Load(a.configPath)
	if err != nil {
		return a.fail(err)
	}
	st, err := state.Load(filepath.Join(a.dataDir, "state.json"))
	if err != nil {
		return a.fail(err)
	}
	var paths []string
	prefer := *browserName
	if fs.NArg() == 1 {
		inst, ok := st.Extensions[fs.Arg(0)]
		if !ok || inst.Path == "" {
			return a.fail(fmt.Errorf("%w: %s is not installed", xerr.ErrNotFound, fs.Arg(0)))
		}
		paths = []string{inst.Path}
		if prefer == "" {
			if e, ok := f.Find(fs.Arg(0)); ok {
				prefer = e.Browser
			}
		}
	} else {
		for _, e := range f.Extensions {
			if inst, ok := st.Extensions[e.Name]; ok && inst.Path != "" {
				paths = append(paths, inst.Path)
			}
		}
		if len(paths) == 0 {
			return a.fail(fmt.Errorf("%w: nothing is installed yet", xerr.ErrNotFound))
		}
	}
	env := browser.Env{GOOS: a.goos, Getenv: a.opt.Getenv, Home: a.opt.Getenv("HOME"), LookPath: a.opt.LookPath, Exists: a.opt.Exists}
	found := browser.Detect(env)
	b, ok := pickBrowser(found, prefer)
	if !ok {
		return a.fail(fmt.Errorf("no Chrome, Chromium, Edge, or Brave binary found"))
	}
	argv, mode, note, err := browser.LaunchPlan(b, paths, *workaround)
	if err != nil {
		return a.fail(err)
	}
	if note != "" {
		fmt.Fprintln(a.stderr, note)
	}
	if mode == "extensions-page" {
		for _, p := range paths {
			fmt.Fprintf(a.stderr, "Load unpacked: %s\n", p)
		}
	}
	cmd := exec.Command(b.Path, argv...)
	browser.Detach(cmd)
	if err := cmd.Start(); err != nil {
		return a.fail(err)
	}
	fmt.Fprintf(a.stdout, "started %s (%s)\n", b.Name, b.Path)
	return 0
}

func pickBrowser(found []browser.Browser, prefer string) (browser.Browser, bool) {
	if len(found) == 0 {
		return browser.Browser{}, false
	}
	if prefer != "" {
		for _, b := range found {
			if b.Name == prefer {
				return b, true
			}
		}
		return browser.Browser{}, false
	}
	for _, b := range found {
		if b.LoadExtension() {
			return b, true
		}
	}
	return found[0], true
}

func (a *app) doctor(args []string) int {
	fs := a.newFlagSet("doctor")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	fmt.Fprintf(a.stdout, "ghx %s\n", a.version())
	fmt.Fprintf(a.stdout, "config: %s\n", a.configPath)
	fmt.Fprintf(a.stdout, "data:   %s\n", a.dataDir)
	if a.tokenSrc == "" {
		fmt.Fprintln(a.stdout, "GitHub credentials: not set (unauthenticated limit is 60 requests/hour; set GITHUB_TOKEN or GH_TOKEN)")
	} else {
		fmt.Fprintf(a.stdout, "GitHub credentials: %s is set\n", a.tokenSrc)
	}
	env := browser.Env{GOOS: a.goos, Getenv: a.opt.Getenv, Home: a.opt.Getenv("HOME"), LookPath: a.opt.LookPath, Exists: a.opt.Exists}
	found := browser.Detect(env)
	fmt.Fprintln(a.stdout, "Browsers:")
	if len(found) == 0 {
		fmt.Fprintln(a.stdout, "  none found on PATH or in standard locations")
	}
	for _, b := range found {
		fmt.Fprintf(a.stdout, "  %s\t%s\tload-extension=%s\n", b.Name, b.Path, b.Load)
		fmt.Fprintf(a.stdout, "    profile: %s\n", b.Data)
	}
	fmt.Fprintln(a.stdout)
	fmt.Fprintln(a.stdout, browser.Explain(a.goos))
	return 0
}

func (a *app) version() string {
	return strings.TrimSpace(func() string {
		// version.Version is in another file's import via cli.go... call through a helper in cli.go
		return versionString()
	}())
}

func (a *app) policy(args []string) int {
	args, err := reorder(args, map[string]bool{"fetch": true, "install-external": true})
	if err != nil {
		return a.fail(err)
	}
	fs := a.newFlagSet("policy")
	fetch := fs.Bool("fetch", false, "download the .crx sibling and read its extension id")
	write := fs.Bool("install-external", false, "write the Linux external_crx JSON into the system directory")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() > 1 {
		fmt.Fprint(a.stderr, commandHelp("policy"))
		return 1
	}
	fmt.Fprintln(a.stdout, browser.Explain(a.goos))
	fmt.Fprintln(a.stdout)
	if fs.NArg() == 0 {
		return 0
	}
	name := fs.Arg(0)
	f, err := config.Load(a.configPath)
	if err != nil {
		return a.fail(err)
	}
	ext, ok := f.Find(name)
	if !ok {
		return a.fail(fmt.Errorf("%w: %s is not tracked", xerr.ErrNotFound, name))
	}
	st, err := state.Load(filepath.Join(a.dataDir, "state.json"))
	if err != nil {
		return a.fail(err)
	}
	inst := st.Extensions[name]
	if *fetch {
		inst, err = a.fetchCRX(ext, inst)
		if err != nil {
			return a.fail(err)
		}
		st.Extensions[name] = inst
		if err := state.Save(filepath.Join(a.dataDir, "state.json"), st); err != nil {
			return a.fail(err)
		}
	}
	if inst.Path != "" {
		fmt.Fprintf(a.stdout, "Unpacked: %s\n", inst.Path)
	} else {
		fmt.Fprintf(a.stdout, "Unpacked: not installed yet. Run: ghx update %s\n", name)
	}
	if inst.PathID != "" {
		fmt.Fprintf(a.stdout, "Unpacked extension id (derived from the path): %s\n", inst.PathID)
	}
	if inst.CRXID != "" {
		fmt.Fprintf(a.stdout, "CRX extension id: %s\n", inst.CRXID)
	} else {
		fmt.Fprintf(a.stdout, "CRX extension id: unknown. If the release ships a .crx, run: ghx policy %s --fetch\n", name)
	}
	if a.goos == "windows" {
		fmt.Fprintln(a.stdout, "\nWindows cannot install a local CRX through the registry or an external JSON file.")
		fmt.Fprintln(a.stdout, "HKLM\\Software\\Google\\Chrome\\Extensions only accepts a Chrome Web Store update_url.")
		fmt.Fprintln(a.stdout, "Use Load unpacked with the directory above.")
		if *write {
			return a.fail(fmt.Errorf("--install-external is a Linux preferences-file install; Windows Chrome rejects local CRX sideload"))
		}
		return 0
	}
	if inst.CRXID == "" || inst.CRXPath == "" || inst.Version == "" {
		return 0
	}
	fmt.Fprintf(a.stdout, "\nLinux external CRX JSON (external_version must be the manifest version %s):\n", inst.Version)
	fmt.Fprintf(a.stdout, "{\n  \"external_crx\": %q,\n  \"external_version\": %q\n}\n", inst.CRXPath, inst.Version)
	which := ext.Browser
	if which == "" {
		which = "chrome"
	}
	dirs := browser.ExternalDirs(a.goos, which)
	fmt.Fprintf(a.stdout, "Place it at <id>.json under one of:\n")
	for _, d := range dirs {
		fmt.Fprintf(a.stdout, "  %s/%s.json\n", d, inst.CRXID)
	}
	fmt.Fprintln(a.stdout, "Quit Chrome completely and start it again. This is the documented Linux method, not an enterprise force-install.")
	if !*write {
		return 0
	}
	var wrote []string
	var failed []string
	for _, d := range dirs {
		dest := filepath.Join(d, inst.CRXID+".json")
		if err := install.WriteExternalJSON(dest, inst.CRXPath, inst.Version); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %s", dest, err.Error()))
			continue
		}
		wrote = append(wrote, dest)
	}
	if len(wrote) == 0 {
		fmt.Fprintln(a.stderr, "ghx: could not write a system external-extension JSON (root is required):")
		for _, line := range failed {
			fmt.Fprintf(a.stderr, "  %s\n", line)
		}
		fmt.Fprintln(a.stderr, "Create the file yourself, for example:")
		if len(dirs) > 0 {
			fmt.Fprintf(a.stderr, "  sudo install -d %s\n", dirs[0])
			fmt.Fprintf(a.stderr, "  sudo tee %s/%s.json >/dev/null <<'EOF'\n", dirs[0], inst.CRXID)
			fmt.Fprintf(a.stderr, "{\n  \"external_crx\": %q,\n  \"external_version\": %q\n}\nEOF\n", inst.CRXPath, inst.Version)
		}
		return 1
	}
	inst.ExternalJSON = wrote
	st.Extensions[name] = inst
	if err := state.Save(filepath.Join(a.dataDir, "state.json"), st); err != nil {
		return a.fail(err)
	}
	sort.Strings(wrote)
	fmt.Fprintln(a.stdout, "Wrote:")
	for _, p := range wrote {
		fmt.Fprintf(a.stdout, "  %s\n", p)
	}
	return 0
}

func (a *app) fetchCRX(ext config.Extension, inst state.Installed) (state.Installed, error) {
	owner, repo, err := config.SplitRepo(ext.Repo)
	if err != nil {
		return inst, err
	}
	rel, asset, err := a.client().Resolve(context.Background(), gh.Query{
		Owner: owner, Repo: repo, Channel: ext.Channel,
		TagPattern: ext.TagPattern, PinTag: ext.PinTag, AssetPattern: ext.AssetPattern,
	})
	if err != nil {
		return inst, err
	}
	crx, ok := gh.SiblingCRX(rel, asset)
	if !ok {
		return inst, fmt.Errorf("%w: %s has no Chrome .crx asset", xerr.ErrNotFound, rel.Tag)
	}
	dest := filepath.Join(a.dataDir, "extensions", ext.Name, "extension.crx")
	if _, err := a.client().DownloadAsset(context.Background(), crx, dest, ""); err != nil {
		return inst, err
	}
	tmp, err := os.MkdirTemp("", "ghx-crx-")
	if err != nil {
		return inst, err
	}
	defer os.RemoveAll(tmp)
	unpacked, err := archive.Unpack(dest, tmp)
	if err != nil {
		return inst, err
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return inst, err
	}
	inst.CRXID = unpacked.CRXID
	inst.CRXPath = abs
	inst.Version = unpacked.Version
	inst.Repo = ext.Repo
	fmt.Fprintf(a.stderr, "fetched %s (%s)\n", crx.Name, unpacked.Version)
	return inst, nil
}
