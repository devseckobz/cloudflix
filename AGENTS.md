# Cloudflix — agent instructions

Self-hosted movie downloader. Go backend (`cmd/cloudflix`, `internal/`) with an
embedded Svelte 5 SPA (`frontend/` → `static/dist`). Linux-only at runtime;
development happens on Windows via WSL Debian.

## "commit it" means: bump the version, commit, and push

When the user says "commit it" (or "commit and push"), do **all four** steps:

1. **Bump the release version** in `.github/workflows/release.yml`:
   - `TAG="vX.Y.Z"` inside the "Choose release version" step.
   - The `tag:` input `description:` example string in the same file.
   - That literal is the project's only source of truth for the version. There
     is no `VERSION` file and `frontend/package.json`'s version is **not** used
     for releases — do not "helpfully" bump it instead.
2. **Run the gates** (see below) and require them green.
3. **Commit** the version bump together with the work, using a `release:` subject
   and the same 2-line diff shape prior releases used.
4. **Push** to `origin main`, then **watch the workflow** and confirm a real
   release was published (see "Verifying a release" below).

### The trap this rule exists to prevent

`release.yml` triggers on every push to `main` and computes `TAG` from that
hardcoded literal. If the literal still names a version that already has a
GitHub release, the job sets `ALREADY_RELEASED=true` and **skips every build and
publish step while reporting success**. The push lands, nothing ships, and the
green checkmark is misleading.

This has happened twice. A push to `main` is only a release if the literal was
bumped in that same push. Never report a release as done without confirming the
tag on the API (see below).

### When NOT to bump

The bump rule is for shipping user-facing work. For a **docs- or
comment-only** change (this file, `README.md`, `docs/`, code comments), do not
bump: a release containing no binary change just adds noise to the update
prompt. Commit and push normally, and expect the release workflow to run and
then skip — that is correct, not a failure.

If you are unsure whether a change counts as shippable: if `go build` output or
`static/dist` would differ, bump; if only Markdown changed, do not.

### Verifying a release

Do not trust the workflow's green check alone — a skipped run is also green:

```bash
gh release view vX.Y.Z            # or the API below
```

```powershell
(Invoke-WebRequest 'https://api.github.com/repos/devseckobz/cloudflix/releases/latest' -UseBasicParsing).Content
```

Confirm: the tag matches, `draft=false`, `prerelease=false`, and the amd64/arm64
tarballs plus their `.sha256` files are present as assets.

## Pre-commit gates

All must pass before committing. Go runs inside WSL; Node runs natively.

```powershell
# Go (no Go on the Windows host — always go through WSL)
wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && go build ./... && go vet ./... && go test ./... -count 1'
wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && go test -race ./cmd/cloudflix -count 1'

# Frontend
npm.cmd run check --prefix frontend      # must be 0 errors AND 0 warnings
npm.cmd test --prefix frontend           # must stay at 60/60
npm.cmd run build --prefix frontend      # writes static/dist

# Shell scripts (mirrors the CI step)
wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && bash -n installer/*.sh scripts/build-media-tools.sh'
```

If `npm.cmd test` fails with `spawn EPERM`, run each file directly instead:
`node frontend/tests/<name>.test.mjs`.

## Repo conventions that bite

- **`static/dist/.gitkeep` must exist.** Vite's `emptyOutDir: true` deletes it on
  every build, so it shows as deleted after `npm run build`. Restore it before
  staging, or the embed target loses its placeholder.
- **`core.autocrlf=true` is set**, so `git status` flags many untouched Go files
  as modified (`i/lf w/crlf`). These are line-ending artifacts, not edits. Verify
  real content with `git diff --stat --ignore-cr-at-eol`, and never commit a
  whole-tree CRLF rewrite — it destroys `git blame`.
- **Do not modify existing `*_test.go` files** unless the assertion is genuinely
  about something you replaced. If you must, say exactly which assertion and why
  in the commit message. Adding new test files is fine.
- `tmp/` is gitignored scratch space (QA harnesses, generated media, benchmarks) —
  never treat its contents as shipped, and never reference it from production
  code. `docs/` **is** committed; put durable notes there.
- `internal/mediatools/payload/*` is gitignored except `README.txt` — the FFmpeg
  binaries are build artifacts injected by CI, not source.
- Match the existing Go style: compact, short names, minimal whitespace.
  Frontend: Svelte 5 runes only, no semicolons, single quotes, 1-space indent in
  `App.svelte`.

## Verification expectations

- **Never report a number you did not measure.** Performance claims need the
  command that produced them.
- Visual claims need real evidence: browser geometry numbers or screenshots of
  the real built bundle, not reasoning about the CSS.
- When a fix is said to resolve a defect, demonstrate the defect is gone at the
  boundary conditions (e.g. narrowest and widest viewport), not just the happy path.
- Report honest gaps. An unmeasured risk stated plainly is more useful than a
  confident guess.

## Task tracking

- Update the todo list to its final truthful state **before** delivering the
  answer, not after. The tracker must never disagree with what was delivered.
- If a teammate dies mid-task and you finish their work, close their shared task
  at the moment you verify it, so nothing is left looking still in progress.

## Release pipeline notes

- `release.yml` builds static FFmpeg/x264 per architecture in the `media-tools`
  matrix job, then publishes. Both architectures must succeed or nothing ships.
- `scripts/build-media-tools.sh` downloads pinned upstream sources and verifies
  SHA-256. `ffmpeg.org` intermittently resets TLS (curl exit 35); the fetch
  already retries all error classes and falls back to `www.ffmpeg.org`. If a
  release fails there, check whether a new mirror is needed rather than assuming
  the code broke.
- The web updater installed on VPS targets `/var/lib/cloudflix/update.request`
  and a root-owned `systemd.path` unit. CI greps for those exact strings; do not
  rename them.
