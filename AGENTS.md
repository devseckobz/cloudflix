# Cloudflix — agent instructions

Self-hosted movie downloader. Go backend (`cmd/cloudflix`, `internal/`) with an
embedded Svelte 5 SPA (`frontend/` → `static/dist`). Linux-only at runtime;
development happens on Windows via WSL Debian.

## "commit it" means: bump the version, commit, and push

When the user says "commit it" (or "commit and push"), do **exactly these
three** steps and then stop:

1. **Bump the release version** in `.github/workflows/release.yml`:
   - `TAG="vX.Y.Z"` inside the "Choose release version" step.
   - The `tag:` input `description:` example string in the same file.
   - That literal is the project's only source of truth for the version. There
     is no `VERSION` file and `frontend/package.json`'s version is **not** used
     for releases — do not "helpfully" bump it instead.
2. **Run the gates** (see below) and require them green.
3. **Push** to `origin main`. Report the tag and commit hash, and stop.

**Do not watch, poll or verify the GitHub run.** The build and publish jobs
belong to GitHub, not to this session. Once the push succeeds, the job is
GitHub's to run; polling it burns minutes and thousands of tokens per release
while adding nothing the user cannot see in their browser. Local gates are our
responsibility and must be green *before* the push; what happens on the runner
afterwards is not.

### The trap this rule exists to prevent

`release.yml` triggers on every push to `main` and computes `TAG` from that
hardcoded literal. If the literal still names a version that already has a
GitHub release, the job sets `ALREADY_RELEASED=true` and **skips every build and
publish step while reporting success**. The push lands, nothing ships, and the
green checkmark is misleading.

This has happened twice. A push to `main` is only a release if the literal was
bumped in that same push — which is exactly why the bump is unconditional.

### Always bump. There is no exception.

**"Commit it" always bumps the version**, including docs-only, comment-only or
one-line changes. The user stated this explicitly after a docs-only commit was
pushed without a bump, and repeated it: *"always bump the version when we
'commit it'"*.

So: every "commit it" bumps `TAG` in `release.yml`, runs the gates, commits and
pushes. Do not reason about whether a change is "worth" a release — that
judgement is not ours to make.

Bumping `TAG` is what makes the release real: if the literal matches an
already-published version, every build and publish step skips while the run
reports success. That is precisely why the bump is unconditional — it removes
the "did we remember to bump?" question rather than leaving it to be judged.

The only things to still avoid: **never bump by two versions in one commit**,
and never reuse a tag that already exists.

### If a release is ever reported as broken

The user watches GitHub themselves; the agent does not poll it. Only look at a
run when the user says a specific release failed, and then read that one run —
not the whole list.

## Pre-commit gates

All must pass before committing. Go runs inside WSL; Node runs natively.

```powershell
# Go (no Go on the Windows host — always go through WSL)
wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && go build ./... && go vet ./... && go test ./... -count 1'
wsl -d Debian -- bash -lc 'cd /mnt/d/cloudflix && go test -race ./cmd/cloudflix -count 1'

# Frontend
npm.cmd run check --prefix frontend      # must be 0 errors AND 0 warnings
npm.cmd test --prefix frontend           # must stay at 83/83 (grows, never shrinks)
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

## Context discipline — keep requests under the payload limit

A long session can hit HTTP 413 `Request Entity Too Large` from the model API.
The error body is the OpenAI-compatible nested form
`{"error":{"code":"413","message":"Request Entity Too Large"}}`, which looks
nothing like this repo's own `{"error":"..."}` shape — so do not go hunting in
`cmd/cloudflix` for it.

Measured on a real 8.9 MB / 5,155-line Cloudflix session:

| Contributor | Share of bytes |
|---|---|
| assistant messages (740 msgs, median 3.8 KB, max 96 KB) | **54 %** |
| tool results (884 calls, avg 2.6 KB) | 25 % |
| user messages | 2 % |

The agent's own prose is the single largest term, so the fix is behavioural:

- **Keep prose tight.** No restating the plan, no re-explaining a fix in three
  places, no long recap of what a command already showed. Say the result once.
- **Never paste large file contents.** Quote the few relevant lines and cite
  `path:line`. A `read` of a 700-line file costs more than the fix does.
- **Bound command output.** Prefer `Select-Object -Last N`, `tail`, `head`,
  `Select-String` over dumping whole logs and directory listings.
- **Do not re-run a passing gate to "confirm".** Each full suite run is
  thousands of tokens of output. Run it when it can fail, not to feel safe.
- If a session is already large, say so rather than loading more bulk into it.

The 413 is a request-size ceiling, not a context-length one: the whole
conversation is resent every turn, so cost compounds with each reply.

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
