# 🎬 Cloudflix

A lightweight, self-hosted movie downloader with a modern web interface. Add magnet links, track downloads, and automatically fetch movie posters, backdrops, and release years with TMDB.

**Go + Svelte 5 · SQLite · Single binary · Linux VPS**

## Install

On Ubuntu/Debian with systemd (amd64 or arm64):

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/install.sh | sudo bash
```

The installer downloads the latest GitHub Release, **asks you to set and confirm a private 6-digit access PIN**, and optionally configures HTTPS for your domain.

Without a domain, Cloudflix listens on `127.0.0.1:3000` for private access. From your own computer:

```bash
ssh -L 3000:127.0.0.1:3000 USER@VPS_IP
```

Open **http://localhost:3000** and enter your PIN. If you configured a domain, use its HTTPS URL instead.

## Update

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/update.sh | sudo bash
```

Existing installations using the old password login are asked to choose a **6-digit PIN once** during their next update. Your movies and settings stay intact.

## Change or reset PIN

```bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/pin.sh | sudo bash
```

PIN entry is hidden. No username is required. After five incorrect attempts, PIN login is locked for 15 minutes. For internet access, use HTTPS; a six-digit PIN is less secure than a long password.

## Configuration

- **TMDB artwork:** set `TMDB_API_KEY` (TMDB API Read Access Token) in `/etc/cloudflix/cloudflix.env`, then restart Cloudflix.
- **Files:** `/var/lib/cloudflix`
- **Service:** `sudo systemctl status cloudflix`
- **Logs:** `sudo journalctl -u cloudflix -n 100 --no-pager`
- **Binaries:** [GitHub Releases](https://github.com/devseckobz/cloudflix/releases)

## Build from source

Requires Go 1.27.3+ and Node.js 22+:

```bash
npm install --prefix frontend
npm run build --prefix frontend
go run ./cmd/cloudflix
```

Set a PIN locally before opening the web interface:

```bash
printf '%s\n' '123456' | go run ./cmd/cloudflix pin set
```

Choose your own PIN; `123456` is only a local development example. Do not reuse it on a VPS.

This project is intended for media you are authorized to download and share. [MIT License](LICENSE). Movie metadata provided by [TMDB](https://www.themoviedb.org/).
