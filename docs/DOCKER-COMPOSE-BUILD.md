# Docker Compose build

## Error: "compose build requires buildx 0.17.0 or later"

If you see this when running `docker-compose build` or `docker compose build`, your Docker Buildx version is older than 0.17.0. The Compose stack still builds with the **legacy Docker builder** (no Buildx required).

### Fix

Use the legacy builder by setting:

```bash
export DOCKER_BUILDKIT=0
```

Then run your build/up as usual:

```bash
docker-compose build
docker-compose up -d redis kafka market-data backtesting prometheus grafana
```

### Applied automatically

- **Script:** `scripts/start-backtest-with-stage.sh` exports `DOCKER_BUILDKIT=0` before calling Compose, so no manual export is needed when using that script.
- **Env file:** If you copy `.env.example` to `.env`, it includes `DOCKER_BUILDKIT=0`. Docker Compose loads `.env` from the project directory, so builds will use the legacy builder.

### Optional: upgrade Buildx

To use BuildKit and a newer Buildx instead, install Docker Buildx 0.17.0 or later (e.g. from [Docker Buildx releases](https://github.com/docker/buildx/releases)) and do **not** set `DOCKER_BUILDKIT=0`.
