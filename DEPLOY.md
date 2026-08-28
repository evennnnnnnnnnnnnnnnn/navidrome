# Deploying this fork

Fork-local. Not upstream documentation.

## The branches

| Branch | Job |
|--------|-----|
| `jml/japanese-learning` | Daily work. Commit here. Nothing deploys from it. |
| `main` | The deploy branch. Default branch. A push here ships to `lenovo-server`. |
| `master` | Untouched upstream mainline, kept only for `gh repo sync`. |

## Shipping

```bash
git checkout main
git merge --ff-only jml/japanese-learning
git push --no-verify origin main
git checkout jml/japanese-learning
```

`--no-verify` is required: the pre-push hook runs `make lint`, and golangci-lint
cannot parse this machine's Go toolchain. It is a local toolchain problem that
predates this work and affects pristine upstream `master` equally. The Docker
build is unaffected, because it compiles inside `golang:1.26-alpine` against
that image's own stdlib.

## What happens next

1. `.github/workflows/deploy.yml` builds `linux/amd64` and pushes to
   `ghcr.io/evennnnnnnnnnnnnnnnn/navidrome`, tagged `:main` and `:sha-<short>`.
   It then asserts yt-dlp, node, ffmpeg and the navidrome binary all answer
   inside the image, so a broken image fails the build rather than the server.
2. `navidrome-deploy.timer` on `lenovo-server` fires every 2 minutes and runs
   `docker compose pull && docker compose up -d` in `/home/yiwenz/navidrome`.
   Compose only recreates the container when the resolved digest changed, so a
   run with nothing new is a no-op costing one registry request.

Typical end-to-end time is the build (several minutes) plus up to 2 minutes.

## Checking a deploy

```bash
ssh lenovo-server 'cd ~/navidrome && docker compose exec -T navidrome /app/navidrome --version'
```

The short SHA in that string is the `main` commit the running container was
built from. `journalctl -u navidrome-deploy` is the poller's log.

## yt-dlp

yt-dlp is baked into the image from PyPI, **not** from `apk`: Alpine 3.20 pins
2024.12.03, which is far too old to extract from YouTube. YouTube breaks
extractors every few weeks, so `deploy.yml` also rebuilds on a weekly schedule.
If YouTube import starts failing, run the workflow manually first:

```bash
gh workflow run deploy.yml -R evennnnnnnnnnnnnnnnn/navidrome
```

## Rolling back

```bash
ssh lenovo-server
cd ~/navidrome
# pin the known-good sha tag, then bring it up
sed -i 's|navidrome:main|navidrome:sha-<good-sha>|' docker-compose.yml
docker compose up -d
```

Remember to put `:main` back afterwards, or the poller will hold that pin
forever.

## Server layout

| Path | Holds |
|------|-------|
| `/home/yiwenz/navidrome/docker-compose.yml` | The service definition |
| `/home/yiwenz/navidrome/data/navidrome.db` | The database, migrated from dev |
| `/home/yiwenz/Music` | The library, mounted at `/music` in the container |

The database stores `library.path` as `/music`, matching the container mount.
If that ever diverges, the scanner marks every track missing and re-imports
them as new, which orphans every music card and furigana binding. Check it
before changing the mount.
