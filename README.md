# ghx

Install and update Chrome extensions from GitHub Releases when they are **not** on the Chrome Web Store. One static binary for Windows and Linux.

## One-click install (Windows)

PowerShell (no token, no `gh`):

```powershell
$dir=Join-Path $env:LOCALAPPDATA 'ghx\bin'; New-Item -ItemType Directory -Force $dir|Out-Null; $exe=Join-Path $dir 'ghx.exe'; Invoke-WebRequest -Uri 'https://github.com/tiamed/ghx/releases/download/v0.1.1/ghx-windows-amd64.exe' -OutFile $exe; $u=[Environment]::GetEnvironmentVariable('Path','User'); if(($u -split ';' | Where-Object { $_ -and $_.TrimEnd('\') -ieq $dir.TrimEnd('\') }).Count -eq 0){ [Environment]::SetEnvironmentVariable('Path', ($u.TrimEnd(';')+';'+$dir), 'User') }; $env:Path="$dir;$env:Path"; ghx --help
```

### Linux

```bash
curl -fsSL -o ghx https://github.com/tiamed/ghx/releases/download/v0.1.1/ghx-linux-amd64
chmod +x ghx && sudo install -m 755 ghx /usr/local/bin/ghx
# arm64: use ghx-linux-arm64 instead
```

All releases: https://github.com/tiamed/ghx/releases

## Quick start

```bash
ghx add https://github.com/owner/my-extension/releases
ghx list
ghx update
ghx path my-extension
```

Nightly / custom asset:

```bash
ghx add https://github.com/owner/nightly-extension/releases \
  --channel nightly --asset 'chrome\.zip$'
```

Then in Chrome: `chrome://extensions` → Developer mode → **Load unpacked** → the folder printed by `ghx path <name>` (or by `add`/`update`). Do that once; later updates only need **Reload** on the card.

## Config and data

| | Linux | Windows |
| --- | --- | --- |
| Config | `~/.config/ghx/config.json` | `%APPDATA%\ghx\config.json` |
| Data | `~/.local/share/ghx` | `%LOCALAPPDATA%\ghx` |

Optional: `GITHUB_TOKEN` / `GH_TOKEN` for private repos or higher API limits (env only, never stored in config).

## Commands

```text
ghx add <repo>     track and install
ghx list           local list
ghx update [name]  update one or all
ghx check [name]   check without downloading
ghx path <name>    unpacked directory
ghx remove <name>  untrack (optional --keep-files)
ghx launch [name]  Chromium --load-extension, or open extensions page
ghx policy [name]  what this OS allows
ghx doctor         paths, token, browsers
```

Useful flags: `-v`, `--channel`, `--asset`, `--dry-run`, `--json`, `--config`, `--data`.

## Build from source

Go 1.22+:

```bash
go build -o ghx ./cmd/ghx
make build-all   # linux amd64/arm64 + windows amd64
```

CI uploads the same binaries as Actions artifacts; `v*` tags publish GitHub Release assets + `SHA256SUMS`.

More detail (channels, asset scoring, Chrome limits): [docs/reference.md](docs/reference.md).

License: MIT.
