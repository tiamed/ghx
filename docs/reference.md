# Reference

## Channels

Drafts are skipped.

| `--channel` | Behavior |
| --- | --- |
| `stable` (default) | Newest non-prerelease (`/releases/latest`) |
| `prerelease` | Newest by `published_at`, including prereleases |
| `nightly` | Prerelease whose tag or title contains `nightly` |
| `preview` | Tag contains `preview` |

`--tag` is a regex on the tag. `--pin` / a `.../releases/tag/...` URL fixes one tag. `--asset` is a regex on the **file name** (not a glob), e.g. `chrome\.zip$`.

## Asset scoring (no `--asset`)

Higher score wins; ties error (or prompt on a TTY):

- `.zip` +50, `.crx` / `.crx3` +30
- name contains `chrome` +80, `chromium` +70, `extension` +40
- source-like or OS/arch tokens penalized
- automatic mode drops firefox / gecko / safari / android names

A chosen file can be saved back as `asset_pattern` with version digits rewritten to `\d+`.

## Verification

If GitHub sends a `digest` (`sha256:`…), the download is hashed; mismatch leaves the old install in place (exit 4). Size must match. Sibling `SHA256SUMS` / `*.sha256` assets are checked when present. Same digest + existing `manifest.json` → skip download. Updates unpack to a staging dir then swap into the same path.

Archives reject `..`, absolute paths, and symlinks. A single top-level folder containing `manifest.json` is hoisted.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | OK (including already up to date) |
| 1 | Usage / runtime error |
| 2 | Tied assets |
| 3 | Not found |
| 4 | Verify failed |
| 5 | Partial failure on multi-update |

`check` exits 0 when the query succeeds, even if an update exists.

## What Chrome allows

ghx does **not** edit Secure Preferences or forge enterprise policy.

| Method | Branded Chrome (Win/Linux) | Chromium / Chrome for Testing |
| --- | --- | --- |
| Load unpacked (Developer mode) | Yes — once, then Reload | Yes |
| `--load-extension` | Removed in Chrome 137+ | Still works (`ghx launch`) |
| Copy into profile `Extensions` | Does nothing | Does nothing |
| External CRX prefs | Windows: no (store URL only). Linux: documented under `/opt/google/chrome/extensions/` etc., needs root | Chromium paths vary |
| Enterprise force-install | Needs http(s) update URL + admin policy | Similar |

Default install path:

```text
<data dir>/extensions/<name>/unpacked
```

`ghx launch` uses `--load-extension` for Chromium / Chrome for Testing; branded Chrome opens `chrome://extensions` and prints the path. `--chrome-workaround` is unsupported and off by default.

Unpacked extension id is path-derived; CRX id is key-derived — they differ.

## Config fields

Unknown JSON keys are rejected (so tokens cannot be stored by accident).

| Field | Meaning |
| --- | --- |
| `name` | Local name `[A-Za-z0-9._-]` |
| `repo` | `owner/name` |
| `channel` | `stable` / `prerelease` / `nightly` / `preview` |
| `tag_pattern` | Regex on tag |
| `asset_pattern` | Regex on asset file name |
| `pin_tag` | Exact tag |
| `browser` | Hint for `launch` |

See `examples/config.json` for a minimal sample.
