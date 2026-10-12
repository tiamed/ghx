# ghx

从 GitHub Releases 安装和更新**没有上架 Chrome 网上应用店**的扩展。一个静态二进制，支持 Windows 和 Linux。

它做三件事：记住你关心的仓库，从 Release 里挑出 Chrome 扩展包，校验后解压到一个固定目录。浏览器那一侧，它只使用 Chrome 仍然允许的安装方式，并写明哪些方式已经被关掉。

## 安装

需要 Go 1.22 或更新版本时可以从源码构建。交叉编译不需要 Windows 机器。

```bash
go build -o ghx ./cmd/ghx
# 三个平台一起编：
make build-all
```

产物：

| 文件 | 系统 |
| --- | --- |
| `dist/ghx-linux-amd64` | Linux x86_64 |
| `dist/ghx-linux-arm64` | Linux arm64 |
| `dist/ghx-windows-amd64.exe` | Windows 64 位 |

CI（`.github/workflows/ci.yml`）在 Linux 上跑测试，并上传上面三个二进制。Windows 二进制是在 Linux 上交叉编译的，CI 不会启动 Windows 跑测试。

Linux 放到 PATH：

```bash
install -m 755 dist/ghx-linux-amd64 ~/.local/bin/ghx
```

Windows（PowerShell），把 exe 放到某个 PATH 目录后：

```powershell
ghx.exe --help
```

### 凭据

访问公开仓库可以不登录。未登录时 GitHub API 大约每小时 60 次请求。私有仓库，或担心限额时：

```bash
export GITHUB_TOKEN=ghp_...    # 或 GH_TOKEN，GITHUB_TOKEN 优先
```

令牌只从环境变量读取，不会写进配置文件。`-v` 日志里授权头显示为 `present` 或 `absent`，值会被替换成 `[REDACTED]`。不要把令牌放进仓库 URL（`https://user:token@github.com/...` 会被拒绝）。

在 GitHub Actions 里，默认的 `GITHUB_TOKEN` 往往只能访问当前仓库。如果 `ghx` 突然 403，先 `unset GITHUB_TOKEN` 再试公开仓库。

## 快速开始

配置在用户目录，不在仓库里。

| | Linux | Windows |
| --- | --- | --- |
| 配置 | `$XDG_CONFIG_HOME/ghx/config.json`，否则 `~/.config/ghx/config.json` | `%APPDATA%\ghx\config.json` |
| 数据 | `$XDG_DATA_HOME/ghx`，否则 `~/.local/share/ghx` | `%LOCALAPPDATA%\ghx` |

可以用 `--config`、`--data`，或环境变量 `GHX_CONFIG`、`GHX_DATA_DIR` 覆盖。

### collectab

[tiamed/collectab](https://github.com/tiamed/collectab/releases) 的正式 Release 带一个 `collectab-<version>-chrome.zip`（当前为 `v0.2.18`）。默认通道就是 stable。

```bash
ghx add https://github.com/tiamed/collectab/releases
ghx list
ghx update collectab          # 同一 digest 时什么都不做
ghx path collectab
```

`add` 会下载并解压。只想先记下仓库时加 `--track-only`。

### danmaku-anywhere

[Mr-Quin/danmaku-anywhere](https://github.com/Mr-Quin/danmaku-anywhere/releases) 同时有三类 Release：

| 通道 | 实际标签 | 附件 |
| --- | --- | --- |
| `stable`（默认） | `v1.5.0`，这是 GitHub 的 latest 非预发布 | `danmaku-anywhere-1.5.0-chrome.zip` 和 `.crx`，另有 firefox zip |
| `nightly` | `nightly-<run id>`，预发布 | `danmaku-anywhere-<version>-chrome.zip` / `.crx` / `firefox.zip` |
| `preview` | `preview-pr-<number>`，预发布 | 同上 |
| `prerelease` | 上面两类里**发布时间最新**的一个 | 目前经常是某次 PR 的 preview，不是 nightly |

标题几乎都叫 “Preview Build …”。工具只根据 **tag** 判断 preview，避免把 nightly 的标题误当成 preview。

只要稳定版：

```bash
ghx add Mr-Quin/danmaku-anywhere
```

要每晚构建：

```bash
ghx add https://github.com/Mr-Quin/danmaku-anywhere/releases \
  --name danmaku-anywhere \
  --channel nightly \
  --asset 'chrome\.zip$'
```

先看会选中哪个文件：

```bash
ghx update danmaku-anywhere --dry-run -v
```

`-v` 会把每个附件的得分打到 stderr。firefox 包在自动选择时直接排除。

两份示例配置都在 [`examples/config.json`](examples/config.json)：

```json
{
  "extensions": [
    {
      "name": "collectab",
      "repo": "tiamed/collectab",
      "channel": "stable",
      "asset_pattern": "chrome\\.zip$"
    },
    {
      "name": "danmaku-anywhere",
      "repo": "Mr-Quin/danmaku-anywhere",
      "channel": "nightly",
      "asset_pattern": "chrome\\.zip$"
    }
  ]
}
```

## 命令

```text
ghx add <repo>       跟踪并安装
ghx list             只看本地配置，不访问网络
ghx remove <name>    取消跟踪并删除解压目录（--keep-files 保留文件）
ghx update [name]    更新一个或全部
ghx check [name]     只查询，不下载
ghx path <name>      打印解压目录
ghx launch [name]    用 Chromium 的 --load-extension 打开，或打开扩展页
ghx policy [name]    打印本机实际允许的安装方式
ghx doctor           路径、令牌是否设置、找到的浏览器
```

全局参数：`-v` / `--verbose`，`--config`，`--data`。`list`、`update`、`check` 支持 `--json`。

仓库可以写成 `owner/name`，或 `https://github.com/owner/name`、`.../releases`、`.../releases/tag/v1.2.3`。带 tag 的 URL 会把该 tag 固定下来（`pin_tag`）。

退出码：

| 码 | 含义 |
| --- | --- |
| 0 | 成功，或已经是这个版本 |
| 1 | 用法错误或其他运行错误 |
| 2 | 多个附件得分相同。非交互时失败；终端里会让你选一个 |
| 3 | 找不到仓库、Release 或扩展名 |
| 4 | 大小、digest 或压缩包校验失败 |
| 5 | 一次更新多个扩展时，有的成功有的失败 |

`check` 发现有新版本时仍然退出 0。它表示“查询成功”，不是“没有更新”。

## 如何挑选 Release 和附件

草稿永远跳过。

| `--channel` | 行为 |
| --- | --- |
| `stable` | 最新的非预发布。实现上先请求 `/releases/latest` |
| `prerelease` | 包括预发布，按 `published_at` 取最新 |
| `nightly` | tag 或标题含 `nightly` 的预发布 |
| `preview` | **tag** 含 `preview` |

还可以加 `--tag`（tag 的正则）和 `--pin`（只装这一个 tag）。`--asset` 是附件**文件名**的正则，不是 glob。`chrome\.zip$` 会在 zip 和 crx 同时存在时锁住 zip。

没写 `--asset` 时的打分（高的赢，平分则报错让你选）：

- `.zip` +50，`.crx` / `.crx3` +30（解压安装优先 zip）
- 文件名含 `chrome` +80，`chromium` +70，`extension` +40
- 像源码包或带 `linux` / `windows` / `amd64` 这类系统名的减分
- 自动模式下，文件名含 firefox、gecko、safari、android 的直接排除

所以 danmaku 的 `*-chrome.zip` 会赢过 `*-chrome.crx` 和 `*-firefox.zip`。只有 crx 时就用 crx，工具会剥掉 CRX 头再解压。显式写出 `firefox\.zip$` 时不再排除 Firefox 包。

终端里遇到平分，选择会被收成一条还能匹配后续版本号的正则（数字换成 `\d+`）写回 `asset_pattern`。

## 校验和幂等

每个附件如果带有 GitHub 的 `digest`（这两个示例仓库现在都有 `sha256:`），下载后重新计算，不一致就删除临时文件，**不会**替换已经装好的目录（退出码 4）。字节数必须和 API 里的 `size` 一致。

同目录如果还有 `SHA256SUMS`、`*.sha256` 这类小文件，也会对照。没有 digest 时，至少比对大小；若响应带了 `ETag`，下次会发 `If-None-Match`。

本地记录的 digest 和远程一致，并且解压目录里还有 `manifest.json`，就跳过下载。更新时解压到临时目录，确认有 manifest 后再换到原来的路径，所以扩展 ID 不会因为目录改名而变。

压缩包会拒绝 `..`、绝对路径和符号链接。单层目录里才有 `manifest.json` 时，会把那一层提到解压根上，这样「加载已解压的扩展程序」不用再点进子目录。

## Chrome 侧载：各系统真正允许什么

ghx **不会**改 Secure Preferences，也**不会**伪造企业策略。下面是浏览器自己的限制。

| 方式 | Windows 上的 Chrome | Linux 上的 Chrome | Chromium / Chrome for Testing |
| --- | --- | --- | --- |
| 开发者模式 → 加载已解压的扩展程序 | 可以。装一次即可。之后点卡片上的「重新加载」 | 同左 | 同左 |
| `--load-extension` | Chrome 137 起，正式版 Chrome 已去掉 | 同左 | 仍然可用。`ghx launch` 会用它 |
| 把文件拷进用户数据目录的 `Extensions` | 不会安装。Chrome 不认这种拷贝 | 同左 | 同左 |
| 外部 CRX（偏好 JSON 的 `external_crx`） | 不行。Windows 的注册表外部安装只能填 Chrome 网上应用店的 `update_url` | 文档仍描述 `/opt/google/chrome/extensions/` 和 `/usr/share/google-chrome/extensions/<id>.json`。需要 root，版本号必须和 manifest 一致，ID 必须是 CRX 公钥算出来的那个 | Chromium 用 `/usr/lib/chromium/extensions` 等目录 |
| 企业策略 `ExtensionInstallForcelist` | 管理员可以强制安装，自建 CRX 还必须有 **http(s)** 更新地址，并放行 `ExtensionInstallSources`。`file://` 不够 | 策略文件在 `/etc/opt/chrome/policies/managed` | `/etc/chromium/policies/managed` |

因此默认行为是：解压到

```text
<数据目录>/extensions/<name>/unpacked
```

然后你在 `chrome://extensions` 打开开发者模式，加载这个目录**一次**。以后 `ghx update` 只换这个目录里的文件。Chrome 可能提示「停用开发者模式扩展」，ghx 不会去关掉这个提示。

`ghx launch` 在找到 Chromium 或 Chrome for Testing 时加上 `--load-extension`。正式版 Chrome 改为打开 `chrome://extensions` 并打印路径。`--chrome-workaround` 会额外加上 `--disable-features=DisableLoadExtensionCommandLineSwitch`，这是社区里的临时办法，可能随时失效，默认关闭。

Edge 和 Brave 是否还认 `--load-extension` 不一定，工具会标成 unknown。

Linux 上如果发布物里有 `.crx`（danmaku-anywhere 有，collectab 目前只有 zip）：

```bash
ghx policy danmaku-anywhere --fetch
sudo 按命令输出，把 JSON 写到系统目录   # 或：
ghx policy danmaku-anywhere --fetch --install-external
```

`--install-external` 写的是文档里的外部 CRX 偏好，不是企业强制安装。写不进去时会打印需要手动执行的命令，而不是假装已经装进浏览器。装好后要完全退出 Chrome 再打开。若你的 Chrome 忽略这个 JSON（例如策略把扩展安装封死了），退回「加载已解压的扩展程序」。

未打包扩展的 ID 由**绝对路径**决定（Chromium 的算法：Linux 对路径做 SHA-256，Windows 对 UTF-16LE 路径做 SHA-256，再映射到 `a`–`p`）。CRX 的 ID 来自公钥，两者不同，不要混用。`ghx update` 会打印路径 ID。

多个配置文件（profile）互不影响：每个要使用扩展的配置文件都要加载一次这个目录。工具不改 `Preferences`。

没有做托盘程序。托盘需要 cgo 和图标库，并不能绕过上面的浏览器限制，反而破坏单文件分发。

## 配置字段

未知字段会报错，避免把令牌误写进 JSON。

| 字段 | 含义 |
| --- | --- |
| `name` | 本地名字，`[A-Za-z0-9._-]` |
| `repo` | `owner/name` |
| `channel` | `stable`、`prerelease`、`nightly`、`preview` |
| `tag_pattern` | tag 正则 |
| `asset_pattern` | 附件名正则 |
| `pin_tag` | 只安装这个 tag |
| `browser` | `chrome`、`chromium`、`edge`、`brave`、`chrome-for-testing`，给 `launch` 用 |

## 开发

```bash
go test ./...
go test -race ./...
make build-all
```

测试不访问网络。选择逻辑用的是这两个仓库当前的真实文件名：`collectab-0.2.18-chrome.zip`，以及 danmaku 的 `*-chrome.zip` / `*-chrome.crx` / `*-firefox.zip`。

许可证：MIT。

---

# English

`ghx` installs and updates Chrome extensions that are **not** published on the Chrome Web Store, using GitHub Releases. One static binary for Windows and Linux.

## Install

Build with Go 1.22 or newer. Cross-compilation does not need a Windows machine.

```bash
go build -o ghx ./cmd/ghx
make build-all
```

| File | System |
| --- | --- |
| `dist/ghx-linux-amd64` | Linux x86_64 |
| `dist/ghx-linux-arm64` | Linux arm64 |
| `dist/ghx-windows-amd64.exe` | 64-bit Windows |

CI (`.github/workflows/ci.yml`) runs the tests on Linux and uploads those three binaries. The Windows binary is cross-compiled; CI does not boot Windows.

Linux:

```bash
install -m 755 dist/ghx-linux-amd64 ~/.local/bin/ghx
```

Windows (PowerShell), after the exe is on `PATH`:

```powershell
ghx.exe --help
```

### Credentials

Public repositories work with no token. Unauthenticated GitHub API use is about 60 requests per hour. For private repositories, or a higher limit:

```bash
export GITHUB_TOKEN=ghp_...    # or GH_TOKEN; GITHUB_TOKEN wins
```

The token is read from the environment only. It is not stored in the config. Verbose logs say `authorization=present` or `absent` and replace the value with `[REDACTED]`. A URL with embedded credentials is rejected.

On GitHub Actions the default `GITHUB_TOKEN` often cannot read other repositories. If requests fail with 403, `unset GITHUB_TOKEN` and try the public repo again.

## Quick start

| | Linux | Windows |
| --- | --- | --- |
| Config | `$XDG_CONFIG_HOME/ghx/config.json`, or `~/.config/ghx/config.json` | `%APPDATA%\ghx\config.json` |
| Data | `$XDG_DATA_HOME/ghx`, or `~/.local/share/ghx` | `%LOCALAPPDATA%\ghx` |

Override with `--config`, `--data`, `GHX_CONFIG`, or `GHX_DATA_DIR`.

### collectab

[tiamed/collectab](https://github.com/tiamed/collectab/releases) publishes a stable `collectab-<version>-chrome.zip` (currently `v0.2.18`). The default channel is stable.

```bash
ghx add https://github.com/tiamed/collectab/releases
ghx list
ghx update collectab          # no-op when the digest is unchanged
ghx path collectab
```

`add` downloads and unpacks. Pass `--track-only` to record the repo without downloading.

### danmaku-anywhere

[Mr-Quin/danmaku-anywhere](https://github.com/Mr-Quin/danmaku-anywhere/releases) publishes three kinds of release:

| Channel | Tags | Assets |
| --- | --- | --- |
| `stable` (default) | `v1.5.0`, which is what GitHub returns as the latest non-prerelease | `danmaku-anywhere-1.5.0-chrome.zip`, `.crx`, and a firefox zip |
| `nightly` | `nightly-<run id>`, prerelease | `danmaku-anywhere-<version>-chrome.zip`, `.crx`, and `firefox.zip` |
| `preview` | `preview-pr-<number>`, prerelease | same shape |
| `prerelease` | whichever of the above was **published last** | often a pull-request preview, not the nightly |

Release titles are almost all “Preview Build …”. Preview matching looks at the **tag only**, so a nightly titled “Preview Build nightly-…” is not treated as a preview.

Stable:

```bash
ghx add Mr-Quin/danmaku-anywhere
```

Nightly:

```bash
ghx add https://github.com/Mr-Quin/danmaku-anywhere/releases \
  --name danmaku-anywhere \
  --channel nightly \
  --asset 'chrome\.zip$'
```

See the choice before downloading:

```bash
ghx update danmaku-anywhere --dry-run -v
```

`-v` prints each asset score on stderr. Firefox packages are excluded unless you opt in with `--asset`.

Both entries live in [`examples/config.json`](examples/config.json):

```json
{
  "extensions": [
    {
      "name": "collectab",
      "repo": "tiamed/collectab",
      "channel": "stable",
      "asset_pattern": "chrome\\.zip$"
    },
    {
      "name": "danmaku-anywhere",
      "repo": "Mr-Quin/danmaku-anywhere",
      "channel": "nightly",
      "asset_pattern": "chrome\\.zip$"
    }
  ]
}
```

## Commands

```text
ghx add <repo>       track and install
ghx list             local config only; no network
ghx remove <name>    untrack and delete unpacked files (--keep-files to keep them)
ghx update [name]    update one extension, or all of them
ghx check [name]     report pending updates; do not download
ghx path <name>      print the unpacked directory
ghx launch [name]    start Chromium with --load-extension, or open the extensions page
ghx policy [name]    print the install methods this OS actually allows
ghx doctor           paths, whether a token is set, detected browsers
```

Global flags: `-v` / `--verbose`, `--config`, `--data`. `list`, `update`, and `check` accept `--json`.

A repo is `owner/name` or `https://github.com/owner/name`, optionally `.../releases` or `.../releases/tag/v1.2.3`. A tag URL sets `pin_tag`.

Exit codes:

| Code | Meaning |
| --- | --- |
| 0 | Success, including “already up to date” |
| 1 | Usage or other runtime error |
| 2 | Several assets tied. Fails when stdin is not a terminal; prompts when it is |
| 3 | Repository, release, or extension name not found |
| 4 | Size, digest, or archive verification failed |
| 5 | A multi-extension update had both successes and failures |

`check` exits 0 when the query succeeds, even if an update is available.

## How a release and asset are chosen

Drafts are always skipped.

| `--channel` | Behavior |
| --- | --- |
| `stable` | Newest non-prerelease. Implemented as `GET /releases/latest` first |
| `prerelease` | Includes prereleases; newest `published_at` wins |
| `nightly` | Prereleases whose tag or title contains `nightly` |
| `preview` | Tag contains `preview` |

`--tag` is a regex on the tag. `--pin` installs one tag. `--asset` is a regex on the **file name**, not a glob. `chrome\.zip$` keeps the zip when a crx is also attached.

Without `--asset`, higher score wins. A tie is an error (or a prompt on a terminal):

- `.zip` +50, `.crx` / `.crx3` +30 (zip is preferred because the install is unpacked)
- `chrome` +80, `chromium` +70, `extension` +40
- source-like names, and OS/arch tokens such as `linux`, `windows`, `amd64`, are penalized
- in automatic mode, firefox, gecko, safari, and android names are dropped

That selects danmaku’s `*-chrome.zip` over `*-chrome.crx` and `*-firefox.zip`. A crx-only release is unpacked by stripping the CRX header. An explicit `firefox\.zip$` pattern opts into the Firefox package.

On a terminal, the file you pick is saved as an `asset_pattern` with version numbers rewritten to `\d+`, so the next release still matches.

## Verification and idempotency

When GitHub sends a `digest` (both sample repos currently send `sha256:`), the download is hashed and rejected on mismatch. The previous unpacked directory is left in place (exit 4). The byte count must match the API `size`.

A sibling `SHA256SUMS` or `*.sha256` asset is checked too. With no digest, size is still checked, and an `ETag` is reused via `If-None-Match` on the next run.

If the stored digest matches and `manifest.json` is still in the unpacked directory, the download is skipped. Updates unpack into a staging directory and swap it into the same path, so the path-derived extension id does not change.

Zip entries that contain `..`, an absolute path, or a symlink are rejected. If `manifest.json` lives in a single top-level folder, that folder is hoisted so “Load unpacked” points at the right directory.

## What Chrome actually allows

ghx does not edit Secure Preferences and does not forge enterprise policy. These limits belong to the browser.

| Method | Chrome on Windows | Chrome on Linux | Chromium / Chrome for Testing |
| --- | --- | --- | --- |
| Developer mode → Load unpacked | Works. Do it once. After later updates, click Reload on the card | Same | Same |
| `--load-extension` | Removed in branded Chrome 137 | Same | Still works. `ghx launch` uses it |
| Copying files into the profile `Extensions` folder | Does not install anything | Same | Same |
| External CRX (`external_crx` in a preferences JSON) | No. Registry external installs accept only a Chrome Web Store `update_url` | Still documented for `/opt/google/chrome/extensions/` and `/usr/share/google-chrome/extensions/<id>.json`. Needs root. The version must match the manifest, and the id must be the CRX key id | Chromium reads `/usr/lib/chromium/extensions` and similar paths |
| Enterprise `ExtensionInstallForcelist` | An admin can force-install. A self-hosted CRX also needs an **http(s)** update URL allowed by `ExtensionInstallSources`. `file://` is not enough | Policy directory: `/etc/opt/chrome/policies/managed` | `/etc/chromium/policies/managed` |

The default is to unpack into:

```text
<data dir>/extensions/<name>/unpacked
```

Open `chrome://extensions`, turn on Developer mode, and load that folder **once**. Later `ghx update` replaces the files in place. Chrome may show “Disable developer mode extensions”. ghx does not try to suppress that prompt.

`ghx launch` passes `--load-extension` when it finds Chromium or Chrome for Testing. Branded Chrome is sent to `chrome://extensions` and the path is printed. `--chrome-workaround` also passes `--disable-features=DisableLoadExtensionCommandLineSwitch`. That is an unsupported, temporary community flag and is off by default.

Edge and Brave may or may not still honor `--load-extension`; they are reported as unknown.

On Linux, when the release includes a `.crx` (danmaku-anywhere does; collectab currently ships only a zip):

```bash
ghx policy danmaku-anywhere --fetch
# or, if this user can write the system directory:
ghx policy danmaku-anywhere --fetch --install-external
```

`--install-external` writes the documented external-CRX preferences file. It is not an enterprise force-install. If the directory is not writable, ghx prints the file and the commands to run; it does not claim the browser installed the extension. Quit Chrome completely and start it again. If that Chrome build ignores the JSON (for example because an install blocklist is set), use Load unpacked.

An unpacked extension id is derived from the **absolute path** (Chromium hashes the path with SHA-256 — UTF-8 on Linux, UTF-16LE on Windows — and maps the first 16 bytes onto `a`–`p`). A CRX id comes from the public key. They differ. `ghx update` prints the path id.

Each Chrome profile must load the folder itself. The tool does not edit `Preferences`.

There is no tray helper. A tray app needs cgo and a GUI library, and it would not change the browser limits above.

## Config fields

Unknown JSON fields are an error, so a token cannot be stored in the file by accident.

| Field | Meaning |
| --- | --- |
| `name` | Local name, `[A-Za-z0-9._-]` |
| `repo` | `owner/name` |
| `channel` | `stable`, `prerelease`, `nightly`, or `preview` |
| `tag_pattern` | Regular expression on the tag |
| `asset_pattern` | Regular expression on the asset file name |
| `pin_tag` | Install only this tag |
| `browser` | `chrome`, `chromium`, `edge`, `brave`, or `chrome-for-testing`, used by `launch` |

## Development

```bash
go test ./...
go test -race ./...
make build-all
```

Tests do not touch the network. Asset fixtures use the real names from these repositories: `collectab-0.2.18-chrome.zip`, and danmaku’s `*-chrome.zip` / `*-chrome.crx` / `*-firefox.zip`.

License: MIT.
