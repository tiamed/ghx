package browser

import "strings"
import "testing"

func TestDetectLinuxAndWindows(t *testing.T) {
	env := Env{
		GOOS: "linux",
		Home: "/home/ada",
		LookPath: func(name string) (string, error) {
			if name == "google-chrome-stable" {
				return "/usr/bin/google-chrome-stable", nil
			}
			if name == "chromium" {
				return "/usr/bin/chromium", nil
			}
			return "", osErr
		},
		Exists: func(string) bool { return false },
	}
	found := Detect(env)
	if len(found) != 2 {
		t.Fatalf("%+v", found)
	}
	if found[0].Name != "chrome" || found[0].Load != "no" {
		t.Fatalf("chrome %+v", found[0])
	}
	if found[1].Name != "chromium" || found[1].Load != "yes" {
		t.Fatalf("chromium %+v", found[1])
	}
	if !strings.Contains(found[0].Data, ".config/google-chrome") {
		t.Fatal(found[0].Data)
	}

	win := Env{
		GOOS: "windows",
		Getenv: func(k string) string {
			if k == "LOCALAPPDATA" {
				return `C:\Users\ada\AppData\Local`
			}
			if k == "ProgramFiles" {
				return `C:\Program Files`
			}
			return ""
		},
		LookPath: func(string) (string, error) { return "", osErr },
		Exists: func(p string) bool {
			return strings.Contains(p, `Program Files\Google\Chrome\Application\chrome.exe`)
		},
	}
	found = Detect(win)
	if len(found) != 1 || found[0].Name != "chrome" || found[0].Load != "no" {
		t.Fatalf("%+v", found)
	}
	if !strings.Contains(found[0].Path, `Program Files\Google\Chrome\Application\chrome.exe`) {
		t.Fatal(found[0].Path)
	}
}

type pathErr string

func (e pathErr) Error() string { return string(e) }

var osErr = pathErr("not found")

func TestLaunchPlan(t *testing.T) {
	chrome := Browser{Name: "chrome", Load: "no", Path: "/usr/bin/google-chrome"}
	args, mode, _, err := LaunchPlan(chrome, []string{"/data/unpacked"}, false)
	if err != nil || mode != "extensions-page" || args[0] != "chrome://extensions" {
		t.Fatalf("%s %v %v", mode, args, err)
	}
	args, mode, note, err := LaunchPlan(chrome, []string{"/data/unpacked"}, true)
	if err != nil || mode != "load-extension" || !strings.Contains(args[1], "/data/unpacked") {
		t.Fatalf("%s %v %v", mode, args, err)
	}
	if !strings.Contains(note, "workaround") {
		t.Fatal(note)
	}
	cr := Browser{Name: "chromium", Load: "yes"}
	args, mode, _, err = LaunchPlan(cr, []string{"/a", "/b"}, false)
	if err != nil || mode != "load-extension" || args[0] != "--load-extension=/a,/b" {
		t.Fatalf("%v %s", args, mode)
	}
}

func TestExternalDirsWindowsEmpty(t *testing.T) {
	if ExternalDirs("windows", "chrome") != nil {
		t.Fatal("windows has no local CRX external dir")
	}
	dirs := ExternalDirs("linux", "chrome")
	if len(dirs) != 2 || !strings.Contains(dirs[0], "google/chrome") {
		t.Fatal(dirs)
	}
}

func TestExplainMentionsLimits(t *testing.T) {
	s := Explain("linux")
	for _, phrase := range []string{"Load unpacked", "Chrome 137", "external", "Windows", "Developer mode"} {
		if !strings.Contains(s, phrase) {
			t.Fatalf("missing %q", phrase)
		}
	}
}
