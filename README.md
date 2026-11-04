# 🎬 Cloudflix

Self-hosted movie downloader with a modern web interface. Paste a magnet link, track downloads, and organize your movie library with TMDB posters and release dates.

## Install

On an Ubuntu/Debian VPS with systemd (amd64 or arm64):

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/install.sh | sudo bash
```

The installer first checks whether **TCP port 80** is free. If it's occupied, it prints the listening process/PID and exits **before installing anything**. Otherwise, it asks you to set and confirm a **6-digit access PIN**.

The installer detects your VPS IPv4 and prints the **actual `http://IP` address** to open. No domain, Caddy, or port number is needed. Your firewall or VPS provider must allow inbound TCP 80; restrict access to your own IP when possible.

> **Security:** Direct HTTP does not encrypt your PIN, session cookies or TMDB token. Avoid untrusted networks. A 6-digit PIN has limited entropy even with login rate limiting.

## Watch & download

Home shows movie posters, release years and TMDB genres (search and 10 movies per page). Select a completed movie to play its MP4 in the built-in player, including matching SRT subtitles and fullscreen support.

Cloudflix inspects magnet metadata **before downloading content**. Magnets without an MP4 are rejected, and only `.mp4` and `.srt` files are selected. Use **Files** in the Downloader queue to inspect included filenames, including rejected torrents. Rejected file listings are temporary (memory-only). After a restart, **Files** can fetch the torrent metadata again from peers without downloading its payload. Deleting a rejected torrent clears its record and cached file list. BitTorrent pieces may contain a few bytes from neighboring files at boundaries. Browser MP4 playback depends on the video's codec.

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
