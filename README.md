# 🎬 Cloudflix

Self-hosted movie downloader with a modern web interface. Paste a magnet link, track downloads, and organize your movie library with TMDB posters and release dates.

## Local development (Windows + WSL Debian)

Use Node.js 22 on Windows and Go 1.27.3 inside WSL Debian. The backend runs
in Linux because MKV preparation uses `/proc/self/fd` and requires `ffmpeg`
and `ffprobe` (Debian package `ffmpeg`).

From PowerShell in the project root:

```powershell
npm.cmd ci --prefix frontend
npm.cmd run check --prefix frontend
npm.cmd test --prefix frontend
npm.cmd run build --prefix frontend
wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && go mod download && go test ./... && go vet ./... && mkdir -p tmp && go build -o tmp/cloudflix ./cmd/cloudflix'
```

Configure your own local six-digit PIN interactively (input is hidden):

```powershell
Read-Host 'Local PIN (six digits)' -MaskInput | wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && CLOUDFLIX_DATA_DIR=./tmp/local-data ./tmp/cloudflix pin set'
```

Start the backend in one terminal:

```powershell
wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && CLOUDFLIX_DATA_DIR=./tmp/local-data CLOUDFLIX_LISTEN=127.0.0.1:3000 ./tmp/cloudflix'
```

Open `http://localhost:3000` for the built app. For frontend hot reload, run
`npm.cmd run dev --prefix frontend` in another terminal and open the Vite URL.
Vite proxies `/api` to port 3000. Local data and binaries stay under ignored
`tmp/`; TMDB credentials can be configured in Settings. Systemd software
updates require an installed VPS service and are unavailable in this local setup.

## VPS installation

On an Ubuntu/Debian VPS with systemd (amd64 or arm64):

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/install.sh | sudo bash
```

The installer first checks whether **TCP port 80** is free. If it's occupied, it prints the listening process/PID and exits **before installing anything**. Otherwise, it asks you to set and confirm a **6-digit access PIN**.

The installer detects your VPS IPv4 and prints the **actual `http://IP` address** to open. No domain, Caddy, or port number is needed. Your firewall or VPS provider must allow inbound TCP 80; restrict access to your own IP when possible.

> **Security:** Direct HTTP does not encrypt your PIN, session cookies or TMDB token. Avoid untrusted networks. A 6-digit PIN has limited entropy even with login rate limiting.

## Watch & download

Downloader shows the free space on the filesystem containing your downloads.
After magnet metadata arrives, Cloudflix checks the remaining size of all selected
MP4, MKV and SRT files before requesting any movie content. It reserves space for
unfinished downloads (including paused jobs) and leaves a 64 MB safety margin.
If the movie will not fit, or disk space cannot be read, the queue shows an error
and does not start the download. Resume checks capacity again. Free disk space
and capacity available for new downloads update live. Other programs can still
consume disk space after a download starts; MKV playback conversion also needs
additional space for its MP4 cache.

Home shows movie posters, release years and TMDB genres (search and 10 movies per page). Play completed **MP4 or MKV** movies in the theater with CC and fullscreen. MKV files are prepared as browser-compatible MP4 when opened; text subtitles embedded in MKV (SRT/ASS) are available through CC. Image-based subtitles such as PGS are not currently supported.

Cloudflix inspects magnet metadata **before downloading content**. Magnets without an MP4 or MKV are rejected, and only `.mp4`, `.mkv` and `.srt` files are selected. Use **Files** in the Downloader queue to inspect included filenames, including rejected torrents. Rejected file listings are temporary (memory-only). After a restart, **Files** can fetch the torrent metadata again from peers without downloading its payload. Deleting a rejected torrent clears its record and cached file list. BitTorrent pieces may contain a few bytes from neighboring files at boundaries. The Downloader has a **Copy magnet** icon next to Delete. MKV playback needs FFmpeg (`ffmpeg` and `ffprobe`) on the VPS; the terminal installer/updater attempts to install it on Ubuntu/Debian. MKV preparation can take time and creates converted files in `/var/lib/cloudflix/playback-cache`.

## Update

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/update.sh | sudo bash
```

Updates download GitHub Release binaries and verify their SHA-256 checksums before replacing the app. The updater preserves movies, PIN and settings, migrates legacy port-3000 installations to port 80, and rolls back the binary if startup fails.

You can also go to **Settings → Software Updates → Check for updates → Update now** to install future releases from the browser. The first one-line update installs the restricted systemd helper needed for in-app updates; no root password or arbitrary shell access is exposed in the UI. Cloudflix briefly restarts during an update.

## Settings

After signing in, open **Settings → TMDB Movie Metadata** to add, replace, or remove your **TMDB v3 API key (32 hex characters)** or **TMDB API Read Access Token**. Cloudflix verifies the token against TMDB and saves it locally. The browser never receives your saved token. [Get your token](https://www.themoviedb.org/settings/api).

## Uninstall / clean reinstall

**Permanent removal**, including all movies, downloads, credentials, and settings:

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/uninstall.sh | sudo bash
```

The script stops Cloudflix and deletes its application files and data immediately, **without asking for confirmation**. Review it before running as root. To remove the application **without deleting** its data:

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/uninstall.sh | sudo env CLOUDFLIX_KEEP_DATA=1 bash
```

## Reset PIN

If a PIN is rejected, use this command on the VPS to replace it. It also clears the login lockout and revokes prior sessions:


```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/pin.sh | sudo bash
```

## Service

`sudo systemctl status cloudflix` · `sudo journalctl -u cloudflix -n 100 --no-pager`

App data: `/var/lib/cloudflix` · Config: `/etc/cloudflix/cloudflix.env` · Port: `80/tcp`

[MIT License](LICENSE) · Use only torrents you are authorized to download or share. Movie metadata from [TMDB](https://www.themoviedb.org/).
