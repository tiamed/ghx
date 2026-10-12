package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRepoRef(t *testing.T) {
	cases := []struct {
		in, owner, repo, pin string
	}{
		{"tiamed/collectab", "tiamed", "collectab", ""},
		{"Mr-Quin/danmaku-anywhere", "Mr-Quin", "danmaku-anywhere", ""},
		{"https://github.com/tiamed/collectab", "tiamed", "collectab", ""},
		{"https://github.com/tiamed/collectab.git", "tiamed", "collectab", ""},
		{"https://github.com/tiamed/collectab/releases", "tiamed", "collectab", ""},
		{"https://github.com/tiamed/collectab/releases/", "tiamed", "collectab", ""},
		{"https://github.com/Mr-Quin/danmaku-anywhere/releases/latest", "Mr-Quin", "danmaku-anywhere", ""},
		{"https://github.com/tiamed/collectab/releases/tag/v0.2.18", "tiamed", "collectab", "v0.2.18"},
		{"github.com/tiamed/collectab/releases", "tiamed", "collectab", ""},
	}
	for _, tc := range cases {
		o, r, p, err := ParseRepoRef(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if o != tc.owner || r != tc.repo || p != tc.pin {
			t.Fatalf("%s -> %s/%s pin %q", tc.in, o, r, p)
		}
	}
	if _, _, _, err := ParseRepoRef("https://user:ghp_SECRET@github.com/a/b"); err == nil {
		t.Fatal("expected credential rejection")
	}
	if _, _, _, err := ParseRepoRef("https://example.com/a/b"); err == nil {
		t.Fatal("expected host rejection")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	in := &File{Extensions: []Extension{{
		Name:         "collectab",
		Repo:         "tiamed/collectab",
		Channel:      "stable",
		AssetPattern: `chrome\.zip$`,
	}, {
		Name:    "danmaku-anywhere",
		Repo:    "Mr-Quin/danmaku-anywhere",
		Channel: "nightly",
	}}}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config should not be group/world readable: %v", info.Mode())
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err, string(b))
	}
	if len(out.Extensions) != 2 || out.Extensions[1].Channel != "nightly" {
		t.Fatalf("%+v", out)
	}
	bad := append([]byte{}, b...)
	bad = append(bad[:len(bad)-2], []byte(`, "token": "x"}`)...)
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("unknown field should fail")
	}
}

func TestPaths(t *testing.T) {
	env := func(k string) string {
		switch k {
		case "HOME":
			return "/home/ada"
		case "APPDATA":
			return `C:\Users\ada\AppData\Roaming`
		case "LOCALAPPDATA":
			return `C:\Users\ada\AppData\Local`
		default:
			return ""
		}
	}
	cfg, data, err := Paths("linux", "", "", env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != filepath.Join("/home/ada", ".config", "ghx", "config.json") {
		t.Fatalf("linux config %s", cfg)
	}
	if data != filepath.Join("/home/ada", ".local", "share", "ghx") {
		t.Fatalf("linux data %s", data)
	}
	cfg, data, err = Paths("windows", "", "", env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != filepath.Join(`C:\Users\ada\AppData\Roaming`, "ghx", "config.json") {
		t.Fatalf("windows config %s", cfg)
	}
	if data != filepath.Join(`C:\Users\ada\AppData\Local`, "ghx") {
		t.Fatalf("windows data %s", data)
	}
	cfg, data, err = Paths("linux", "/tmp/c.json", "/tmp/data", env)
	if err != nil || cfg != "/tmp/c.json" || data != "/tmp/data" {
		t.Fatalf("flags %s %s %v", cfg, data, err)
	}
}
