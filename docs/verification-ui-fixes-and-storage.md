# Cloudflix — UI fixes, session detection and storage cleanup: verification record

Companion to [`verification-60fps-and-ui.md`](verification-60fps-and-ui.md).
Everything below was produced by commands and browser probes actually run on
this machine (Windows host + WSL Debian, Go 1.27.3, Node 22, ego-browser
Chromium). No number here is estimated.

Shipped as **v0.1.25**, **v0.1.26** and **v0.1.27**.

---

## 1. Gate state at v0.1.27

| Gate | Command | Result |
|---|---|---|
| Go build | `go build ./...` | PASS |
| Go vet | `go vet ./...` | PASS |
| Go tests | `go test ./... -count 1` | PASS (4 pkgs) |
| Go race | `go test -race ./cmd/cloudflix -count 1` | PASS (~11 s) |
| Frontend types | `npm run check` | 0 errors, 0 warnings |
| Frontend tests | `npm test` | 83/83 pass |
| Frontend build | `npm run build` | PASS (457.5 kB js / 122.9 kB css) |
| Shell syntax | `bash -n installer/*.sh scripts/build-media-tools.sh` | PASS |

Frontend tests grew 60 → 83 across these releases.

---

## 2. v0.1.25 — Downloader and Sessions

### The stat cards were rendering 62% too tall

`.stat` was authored as a compact horizontal chip (`display:flex` + `gap`), but
the shadcn `Card` primitive ships `flex flex-col`. The primitive's direction won,
so each card became a tall centred stack: icon above a centred label/value.

Measured before and after at 1310 px:

| | height | icon position |
|---|---|---|
| before | 118.1 px | above the text |
| after | 73.1 px | left of the text |

Fix: state `flex-direction: row` explicitly in `.stat` rather than relying on
the primitive's default.

Cards 4 and 5 also used the **same** `HardDrive` glyph. They now use
`HardDriveDownload` and `Database`, so all five icons are distinct.

### Duplicate connection indicator

The Downloader rendered its own "Connected" pill beside the page title while the
topbar already showed one. Removed, along with the now-unused
`.downloader-online` rule. Measured: 1 indicator on the page, was 2.

### Session browser detection

The inline `browserLabel()` produced only `Browser · Platform`, so it could not
tell apart the clients people actually confuse. Replaced with
`frontend/src/lib/user-agent.js`:

- browser **version** (`Chrome 131`, not `Chrome`)
- **Android version** parsed from the `Android` token, not the `Linux` token
- **iPadOS vs iOS** — both used to render as the identical `Safari · iOS`
- **Android WebView** (`; wv)`) distinguished from real Chrome, plus the model
- Chromium forks matched before Chrome and Chrome before Safari: Edge, Opera
  Samsung Internet, Vivaldi, Headless Chrome
- iOS Chrome (`CriOS`) reported as Chrome, not Safari
- `curl`, unknown and empty agents degrade without throwing

Example rows, verified in a real browser at 390/768/1024/1310/1440 px:

```
Chrome 131 · macOS · macOS 10.15.7
Chrome 131 · Android 14 · SM-S918B
Chrome 131 · Android 13 · WebView · Pixel 7
Safari 18 · iOS 18.1
Safari 17 · iPadOS 17.5
```

### "Idle" beside a watch-progress line

A row holding a resume point but not currently playing said **Idle** while the
line beneath it showed a movie and a timestamp. Such rows now read **Paused**
(amber). Verified: `contradictions: []` at every width.

### Cross-check: the reported clipping was not reproducible

The "Space for downloads" label in the report was **not** clipped. Pixel
analysis of the supplied 2x screenshot found all five cards at 234 px wide and
121 px tall, and a geometry sweep of the rebuilt bundle at 1100–1920 px found
`labelClipped=false` and `labelOverflowPx<=0.5` throughout. No change was made
for a defect that could not be reproduced.

Genre badges, similarly, ellipsize gracefully rather than hard-clipping — an 8x
crop confirms the ellipsis is painted.

---

## 3. v0.1.26 — delete the downloaded files

`engine.action(hash,"remove")` deleted the SQLite row and dropped the torrent
but never touched the filesystem. There was no `os.Remove`/`RemoveAll` anywhere
in the delete path, so every delete leaked a movie. The confirm dialog said so
plainly: *"Downloaded files are retained on the server."*

The dialog now offers both outcomes:

| Choice | Effect |
|---|---|
| Remove from queue | deletes the entry, keeps the files (old behaviour) |
| Delete files too | also unlinks the movie and subtitle files |

The server deletes only when the request carries `{"deleteFiles":true}`, so
every existing caller keeps the old behaviour. `confirmDialog()` gained an
optional `alternateLabel` and resolves `cancel`/`confirm`/`alternate` for
three-button dialogs, still boolean for two-button ones.

Two call sites compared with `if(!ok)` were changed to `if(ok!==true)`: the
truthy `'cancel'` string would otherwise have read as confirmation.

Deletion is narrow by construction:

- paths come from the torrent's own metadata or inspected manifest, never the
  request
- each path passes `allowedMediaPath`, and every unlink goes through `os.Root`
- a second torrent claiming the same filename keeps that file
- rejected torrents never downloaded payload, so they delete nothing
- already-missing files are not an error
- cached transcodes for the deleted files are dropped

---

## 4. v0.1.27 — clean up orphaned files

Deleting with "keep files" still left bytes the UI could not reclaim. The
Downloader now scans for files no queued torrent owns and offers one
"Clean up <size>" action.

`GET /api/storage/leftovers` is a read-only dry run;
`POST /api/storage/cleanup` deletes. A test pins each verb to its handler so a
refactor cannot make the dry run destructive.

The non-obvious constraint: **anacrolix defaults to `UsePartFiles: true`**, so a
live download is on disk as `movie.mkv.part` while its manifest names
`movie.mkv`. Verified by reading
`torrent@v1.59.1/storage/file-client.go`. Treating `.part` files as garbage
would have corrupted active downloads, so both names count as owned.

Other guards:

- only `.mp4`/`.mkv`/`.srt` are candidates, so a stray text file is never deleted
- a torrent still fetching metadata makes the file list unknowable; the scan
  refuses rather than guess, and the button stays hidden
- paths are re-checked against the live queue immediately before unlinking
- symlinks are neither followed nor deleted; the downloads root is never removed
- emptied release folders are pruned deepest-first

### The magnet button grew with the textarea

`align-items: stretch` on `.magnet-entry-row` made "Add download" track the
user-resizable textarea. The row now aligns to the top and the button holds a
fixed 42 px, matching the field's `min-height`.

Measured at 390 px and 1440 px: with the textarea dragged from 42 px to 130 px,
the button height stayed exactly **42 px (`grewPx: 0`)**.

---

## 5. Released-binary end-to-end

Downloaded the published `v0.1.27` amd64 tarball, verified SHA-256, booted it
with `CLOUDFLIX_LISTEN=127.0.0.1:3402` on a scratch data dir, configured the PIN
via `cloudflix pin set`, logged in, then:

1. planted orphaned `Orphan.2019/movie.mkv` (3 MB) and `movie.en.srt` (60 kB)
   plus an orphaned `.part`, with a `notes.txt` decoy
2. `GET /api/storage/leftovers` → `{"count":3,"bytes":3160000,...}` and the files
   were still on disk
3. `POST /api/storage/cleanup` → `{"deleted":3,"bytes":3160000}`
4. the orphans and their emptied folder were gone; `notes.txt` and the
   downloads root survived
5. a second `GET` → `{"count":0,"bytes":0}`

---

## 6. Re-run

```powershell
wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && go build ./... && go vet ./... && go test ./... -count 1'
wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && go test -race ./cmd/cloudflix -count 1'
npm.cmd run check --prefix frontend
npm.cmd test --prefix frontend          # expect 83/83
npm.cmd run build --prefix frontend
```

The browser probes live in `tmp/qa2/` (gitignored scratch): `diag-final.mjs`
for the UI geometry, `verify-delete-ui3.mjs` for the delete dialog, and
`verify-cleanup-ui.mjs` for the cleanup button and magnet form.
