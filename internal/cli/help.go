package cli

const rootHelp = `ghx — install and update Chrome extensions from GitHub Releases

Usage:
  ghx [global flags] <command> [flags]

Commands:
  add <repo>      Track a GitHub repo and install the latest matching release
  list            Show tracked extensions
  remove <name>   Stop tracking an extension and delete local files
  update [name]   Download and unpack updates (all extensions when name is omitted)
  check [name]    Ask GitHub what would change, without downloading
  path <name>     Print the unpacked directory
  launch [name]   Start Chromium with --load-extension, or open chrome://extensions
  policy [name]   Explain sideload limits and optional Linux external CRX install
  doctor          Show paths, credentials, and detected browsers
  version         Print ghx version
  help [command]  Show help

Global flags:
  -v, --verbose       Log API calls and asset decisions to stderr (tokens are redacted)
      --config PATH   Config file (default: OS config dir/ghx/config.json)
      --data PATH     Data directory (default: OS data dir/ghx)

Examples:
  ghx add https://github.com/owner/my-extension/releases
  ghx add https://github.com/owner/nightly-extension/releases --channel nightly
  ghx update
  ghx update nightly-extension --dry-run

Exit codes:
  0  success, or already up to date
  1  usage or runtime error
  2  several assets match; pass --asset or run in a terminal to choose
  3  repository, release, or extension not found
  4  download failed size, digest, or archive checks
  5  a batch update had both successes and failures

Set GITHUB_TOKEN or GH_TOKEN for private repos and higher rate limits.
The token is read from the environment only. It is never written to the config
or printed in logs.
`

func commandHelp(name string) string {
	switch name {
	case "add":
		return `Usage: ghx add <repo-or-url> [flags]

  <repo> is owner/name or a https://github.com/owner/name[/releases[/tag/TAG]] URL.

Flags:
  --name NAME         Config name (default: repository name)
  --channel CHANNEL   stable (default), prerelease, nightly, or preview
  --prerelease        Same as --channel prerelease
  --tag REGEX         Release tag must match
  --asset REGEX       Asset file name must match (for example chrome\.zip$)
  --pin TAG           Install this tag only
  --browser NAME      Remembered for launch: chrome, chromium, edge, brave, chrome-for-testing
  --track-only        Save the config and do not download
  --dry-run           Resolve the asset and do not download
  --force             Replace an existing config entry

Some projects publish nightlies and pull-request previews as pre-releases,
plus an older stable tag such as v1.2.0. The default channel is stable. Use
--channel nightly for nightly-* tags. --channel prerelease follows the newest
published release, which is often a preview-pr-* build.

A project that only publishes stable *-chrome.zip assets needs no channel flag.
`
	case "list":
		return "Usage: ghx list [--json]\n\nList tracked extensions from the local config. This does not contact GitHub.\n"
	case "remove", "rm":
		return "Usage: ghx remove <name> [--keep-files]\n\nRemove the extension from the config and delete its unpacked files.\nChrome keeps a loaded extension until you remove it at chrome://extensions.\n"
	case "update":
		return "Usage: ghx update [name] [--dry-run] [--json]\n\nDownload the selected asset, verify its size and GitHub digest, and unpack it\ninto a stable directory. A second run with the same digest does nothing.\n"
	case "check":
		return "Usage: ghx check [name] [--json]\n\nContact GitHub and report pending updates. Exit 0 when the check itself succeeds,\nincluding when an update is available.\n"
	case "path":
		return "Usage: ghx path <name>\n\nPrint the absolute unpacked directory.\n"
	case "launch":
		return `Usage: ghx launch [name] [--browser NAME] [--chrome-workaround]

Start a detected browser. Chromium and Chrome for Testing get --load-extension.
Branded Chrome 137+ ignores that flag, so ghx opens chrome://extensions and
prints the unpacked path instead.

--chrome-workaround adds --disable-features=DisableLoadExtensionCommandLineSwitch.
That flag is an unsupported community workaround and may stop working.
`
	case "policy":
		return `Usage: ghx policy [name] [--fetch] [--install-external]

Print what this operating system actually allows. With a name, also print the
unpacked path and, when a CRX id is known, the Linux external_crx JSON.

--fetch downloads the .crx asset when the release publishes one next to the zip.
--install-external writes <id>.json under the system Chrome/Chromium extensions
directory. It needs permission to write there. Windows cannot use this method.
`
	case "doctor":
		return "Usage: ghx doctor\n\nShow config path, data path, whether a GitHub token is set, and detected browsers.\n"
	default:
		return rootHelp
	}
}
