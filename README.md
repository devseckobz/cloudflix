# 🎬 Cloudflix

A self-hosted **movie download manager** built with **Go, Svelte 5, anacrolix/torrent, SQLite and TMDB**. Download your own legally shareable media using magnet URIs and manage your library from a modern responsive dashboard.

> **Status:** Early alpha. The first release will be published when GitHub Actions builds and tests complete successfully. Never execute a downloaded shell script without reviewing it first.

## Features

- 🔒 First-run administrator password setup, Argon2id hashing, HTTP-only sessions
- 🎞️ A dark cinematic homepage with TMDB posters, backdrops and movie years
- 🧲 Magnet URI downloading with the BitTorrent engine running **inside Go**
- 📊 Live download progress, pause/resume/remove controls and file retrieval
- 💾 SQLite records, torrent session restoration, no mandatory qBittorrent or Docker
- 📦 **One compiled Go binary** with the Svelte UI embedded
- 🛠️ Systemd installation, optional Caddy HTTPS, checksummed GitHub release downloads
- 🔄 One-line update script with service-health check and binary rollback

## Quick install on a VPS

Requirements: **Linux (Ubuntu/Debian recommended), systemd, amd64 or arm64, root/sudo, enough disk space**, and curl or wget. Releases are generated automatically by [GitHub Actions](https://github.com/devseckobz/cloudflix/actions). You do not need Go or Node.js on the VPS.

### Install (one line)

Review the [installer source](installer/install.sh) before running it.

~~~bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/install.sh | sudo bash
~~~

The default configuration binds Cloudflix **only to 127.0.0.1:3000** for safety. Access the setup page through an SSH tunnel:

~~~bash
ssh -L 3000:127.0.0.1:3000 USER@YOUR_VPS_IP
~~~

Then visit **http://localhost:3000** and choose an administrator password (12+ characters).

### Install with a domain and automatic HTTPS

Point an A/AAAA record at your VPS, then run:

~~~bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/install.sh | sudo env CLOUDFLIX_DOMAIN=movies.example.com TMDB_API_KEY=YOUR_TMDB_BEARER_TOKEN bash
~~~

This installs/configures Caddy and creates an HTTPS reverse proxy on the chosen domain. Ensure ports **80 and 443** reach the VPS and your firewall allows them. Cloudflix's application backend remains bound to localhost.

**Never publish a real TMDB token in shell history or scripts committed to Git.** Instead, omit `TMDB_API_KEY` from the command and set it afterwards in `/etc/cloudflix/cloudflix.env` with root-only permissions, then run `sudo systemctl restart cloudflix`.

TMDB movie artwork requires an API read-access bearer token. Without one, magnet downloading still functions but the library uses fallback artwork and parsed torrent filenames.

### Update (one line)

~~~bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/update.sh | sudo bash
~~~

Updates download the **latest published non-prerelease GitHub Release**, verify the checksum, swap the Go executable, restart the service, and roll back the previous binary if the local health check fails. Your movies, SQLite database, session cookies, configuration, and administrator password are preserved.

Pin a specific version if needed:

~~~bash
wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/update.sh | sudo env CLOUDFLIX_VERSION=v0.1.0 bash
~~~

### Services, paths and troubleshooting

| Path / command | Purpose |
|---|---|
| `/usr/local/bin/cloudflix` | Compiled app |
| `/etc/cloudflix/cloudflix.env` | Protected environment settings |
| `/var/lib/cloudflix` | Movies and SQLite database |
| `sudo systemctl status cloudflix` | Service status |
| `sudo journalctl -u cloudflix -n 100 --no-pager` | Logs |
| `sudo systemctl restart cloudflix` | Restart |
| `https://github.com/devseckobz/cloudflix/releases` | Prebuilt binaries |

## Development

Requires **Go 1.27.3+** and **Node.js 22+**.

~~~bash
git clone https://github.com/devseckobz/cloudflix.git
cd cloudflix
npm install --prefix frontend
npm run build --prefix frontend
go mod tidy
go run ./cmd/cloudflix
~~~

The Go application listens on `127.0.0.1:3000` and serves embedded Svelte files. For frontend hot-reload, use `npm run dev --prefix frontend` alongside the Go service and open Vite's dev-server URL. Vite proxies `/api` requests to Go.

To run tests and compilation:

~~~bash
npm run check --prefix frontend
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o cloudflix ./cmd/cloudflix
~~~

## Releasing

CI runs on each push and pull request. The release workflow builds Linux **amd64** and **arm64** binaries and publishes versioned, SHA-256-checksummed `.tar.gz` artifacts to GitHub Releases. The first successful push publishes **v0.1.0** if it does not already exist. For later releases, open **Actions → Build and Publish Release → Run workflow**, provide a new semver tag such as `v0.1.1`, and run it on `main`. A workflow run will build and upload the new version.

## Security notes

- Run behind HTTPS and use strong administrator credentials. There is deliberately **no default password**.
- Keep the SQLite DB and configuration outside the downloaded-content folder. The installer uses a dedicated non-root service account.
- Downloaded media are untrusted. Do not execute files obtained from torrents.
- HTTP download routes are authenticated and constrained to known torrent files inside the downloads root.
- Do not use Cloudflix to download or share files you do not have rights to access or distribute.
- **Alpha limitations:** no multi-user access control, incomplete torrent priority/queue controls, no disk quota enforcement, and no upload/seeding management UI. Test in a nonproduction VPS first.

Inspired by the [Cloud Torrent](https://github.com/jpillora/cloud-torrent) architecture. Cloudflix is an independent implementation and does not copy Cloud Torrent source code. The embedded torrent engine uses [anacrolix/torrent](https://github.com/anacrolix/torrent), licensed MPL-2.0. Movie metadata and images are provided by [TMDB](https://www.themoviedb.org/) and subject to its terms.
