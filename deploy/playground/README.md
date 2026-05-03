# Mnemos public playground

A "try before you install" deployment of Mnemos. Anyone on the
internet can hit the URL, get a token, and exercise the HTTP API
without installing anything.

## What it is

- Mnemos with the **in-memory backend** — every event evaporates on
  container restart. No persistent data, no privacy footprint.
- nginx in front, rate-limited to ~30 req/min per IP.
- One shared read-write JWT, served at `GET /token`. Rotates on
  container restart.
- Designed to restart **daily** (scheduled by your orchestrator) so
  the token rotates and the data resets.

This is **not** a free hosted Mnemos. It's a sandbox. Production users
self-host their own.

## Deploy

```bash
export MNEMOS_JWT_SECRET=$(openssl rand -hex 32)
docker compose -f deploy/playground/docker-compose.yml up -d
```

Then point DNS / your reverse proxy at the host. nginx listens on
`:8080`. Caddy or Cloudflare handles TLS.

Schedule a daily restart so state and token rotate:

```cron
# /etc/cron.d/mnemos-playground
0 3 * * *  root  cd /opt/mnemos && docker compose -f deploy/playground/docker-compose.yml restart
```

## Use it

```bash
PLAYGROUND=https://playground.mnemos.dev
TOKEN=$(curl -s $PLAYGROUND/token)

# Append an event
curl -sX POST $PLAYGROUND/v1/events \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"events":[{
    "id":"'$(uuidgen)'",
    "run_id":"playground-demo",
    "source_input_id":"playground-curl",
    "content":"hello mnemos",
    "timestamp":"'$(date -u +%FT%TZ)'",
    "metadata":{}
  }]}'

# Read it back
curl -s "$PLAYGROUND/v1/events?run_id=playground-demo" -H "Authorization: Bearer $TOKEN"
```

## Limits

- 30 req/min per IP across the API
- 5 req/min for `/token`
- Token TTL = 24h
- All data resets on container restart (default: daily)

These are deliberately tight. The playground is for shape-checking,
not workload. Run your own Mnemos for anything that matters.
