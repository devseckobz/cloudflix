# 🎬 Cloudflix

Self-hosted movie downloader for a Linux VPS: paste a magnet, track downloads, browse a TMDB-backed library, and stream in the browser.

## Quick start

Ubuntu/Debian with systemd on amd64. Data `/var/lib/cloudflix`, config `/etc/cloudflix/cloudflix.env`, port `80/tcp`.

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/install.sh | sudo bash
```

The installer aborts if TCP port 80 is occupied, asks you to set a 6-digit PIN, then prints the `http://<VPS IPv4>` address to open. No domain is needed; allow inbound TCP 80, ideally only from your own IP.

## Features

- **Downloads:** metadata is fetched before any movie content; only `.mp4`, `.mkv` and `.srt` files are selected, and magnets without an MP4/MKV are rejected.
- **Capacity:** the Downloader shows free disk space; capacity is checked before download and again on resume, reserving unfinished and paused jobs plus a 64 MB safety margin. If the movie will not fit or disk space cannot be read, the queue shows an error and starts nothing.
- **Home and queue:** posters, release years, TMDB genres, search, 10 movies per page. Queue Files lists included filenames (memory-only; re-fetched from peers after restart) and rejected torrents never download payload; Copy magnet sits next to Delete.
- **Theater:** compatible MP4 downloads play directly. After an MKV file verifies, Cloudflix starts preparing a permanent MP4 and extracting supported text subtitles. Once the torrent and its conversions are complete, Cloudflix records the files in its library and removes the original MKVs. Failed conversions keep their sources. Converted files live in `/var/lib/cloudflix/library`.
- **Media tools:** release binaries embed private, static `ffmpeg`/`ffprobe`, extracted to `/var/lib/cloudflix/runtime-tools` on first MKV playback; no system FFmpeg is needed. Built from pinned FFmpeg and x264 sources by `scripts/build-media-tools.sh`; releases ship their sources, recipe, checksums and GPL notices. Cloudflix is MIT; the tools keep their own licenses.
- **TMDB:** open **Settings → TMDB Movie Metadata** to add a 32-character v3 API key or an API Read Access Token. It is verified against TMDB and stored server-side; the browser never receives it. [Get a token](https://www.themoviedb.org/settings/api).

## Update

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/update.sh | sudo bash
```

Verifies SHA-256 checksums, preserves movies/PIN/settings, migrates legacy port-3000 installs to port 80, and rolls back if startup fails.

The one-line install/update also installs a restricted systemd helper for **Settings → Software Updates → Check for updates → Update now**. No root password or arbitrary shell access is exposed; Cloudflix restarts briefly during an update.

## Uninstall / reset PIN

```bash
# Permanent: stops the service and deletes app files, movies, PIN and settings (no confirmation)
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/uninstall.sh | sudo bash
# Remove the app but keep data
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/uninstall.sh | sudo env CLOUDFLIX_KEEP_DATA=1 bash
# Replace a rejected PIN; also clears the lockout and revokes sessions
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/pin.sh | sudo bash
```

## Local development (Windows + WSL Debian)

Node.js 22 on Windows and Go 1.27.3 in WSL Debian. The backend is Linux-only: MKV preparation uses `/proc/self/fd` and needs `ffmpeg`/`ffprobe` (Debian package `ffmpeg`).

```powershell
npm.cmd ci --prefix frontend
npm.cmd run check --prefix frontend
npm.cmd test --prefix frontend
npm.cmd run build --prefix frontend
wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && go mod download && go test ./... && go vet ./... && mkdir -p tmp && go build -o tmp/cloudflix ./cmd/cloudflix'
Read-Host 'Local PIN (six digits)' -MaskInput | wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && CLOUDFLIX_DATA_DIR=./tmp/local-data ./tmp/cloudflix pin set'
wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && CLOUDFLIX_DATA_DIR=./tmp/local-data CLOUDFLIX_LISTEN=127.0.0.1:3000 ./tmp/cloudflix'
```

Open `http://localhost:3000`; `npm.cmd run dev --prefix frontend` proxies `/api` to port 3000. Local data stays in ignored `tmp/`; in-app systemd updates need a VPS install.

## Service

| Task | Command |
| --- | --- |
| Status | `sudo systemctl status cloudflix` |
| Logs | `sudo journalctl -u cloudflix -n 100 --no-pager` |

> **Security:** plain HTTP does not encrypt the PIN, session cookies or TMDB token. Avoid untrusted networks; a 6-digit PIN has limited entropy even with login rate limiting.

[MIT License](LICENSE) · Use only torrents you are authorized to download or share. Movie metadata from [TMDB](https://www.themoviedb.org/).
