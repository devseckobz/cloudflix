# 🎬 Cloudflix

Self-hosted movie downloader with a modern web interface. Paste a magnet link, track downloads, and organize your movie library with TMDB posters and release dates.

**Go · Svelte 5 · SQLite · One binary · Linux VPS**

## Install

On an Ubuntu/Debian VPS with systemd (amd64 or arm64):

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/install.sh | sudo bash
```

The installer first checks whether **TCP port 80** is free. If it's occupied, it prints the listening process/PID and exits **before installing anything**. Otherwise, it asks you to set and confirm a **6-digit access PIN**.

Open **`http://YOUR_VPS_IP`** to sign in. No domain, Caddy, or port number is needed. Your firewall or VPS provider must allow inbound TCP 80; restrict access to your own IP when possible.

> **Security:** Direct HTTP does not encrypt your PIN, session cookies or TMDB token. Avoid untrusted networks. A 6-digit PIN has limited entropy even with login rate limiting.

## Update

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/update.sh | sudo bash
```

Updates use verified binaries from [GitHub Releases](https://github.com/devseckobz/cloudflix/releases). The updater migrates previous default port-3000 installations to port 80 and preserves movies, PIN, and settings. It exits if another service already owns port 80.

## Settings

After signing in, open **Settings → TMDB Movie Metadata** to add, replace, or remove your **TMDB v3 API key (32 hex characters)** or **TMDB API Read Access Token**. Cloudflix verifies the token against TMDB and saves it locally. The browser never receives your saved token. [Get your token](https://www.themoviedb.org/settings/api).

## Uninstall / clean reinstall

**Permanent removal**, including all movies, downloads, credentials, and settings:

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/uninstall.sh | sudo bash
```

The script deletes Cloudflix immediately, without confirmation. **Review it before running as root.** To remove the application **without deleting** its data:

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/uninstall.sh | sudo env CLOUDFLIX_KEEP_DATA=1 bash
```

## Reset PIN

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/pin.sh | sudo bash
```

## Service

`sudo systemctl status cloudflix` · `sudo journalctl -u cloudflix -n 100 --no-pager`

App data: `/var/lib/cloudflix` · Config: `/etc/cloudflix/cloudflix.env` · Port: `80/tcp`

[MIT License](LICENSE) · Use only torrents you are authorized to download or share. Movie metadata from [TMDB](https://www.themoviedb.org/).
