# mf-observability — Prometheus + Grafana

Metrics for the Go backend, in their own containers.

```
mf-backend  <--scrape--  prometheus  <--query--  grafana
  /metrics                  :9090                 :3001
```

The direction matters and is the thing most often drawn backwards: **the
application never pushes anywhere.** Prometheus pulls on a schedule; Grafana
only ever talks to Prometheus. A consequence worth having: if this whole stack
is down, the backend neither knows nor cares.

## What is measured

**HTTP surface** — request count by route, method and status; a latency
histogram; requests in flight.

Labels carry the chi *route pattern* (`/llm/runs/{id}`), never the raw path. A
raw path mints a new time series for every run id anyone fetches, and unbounded
label cardinality is the standard way to kill a Prometheus server. Unmatched
requests collapse to `route="unmatched"` for the same reason — otherwise a
scanner probing random URLs writes the series.

**Generation** — duration, tokens and failures, labelled by `target`
(`browser` / `server`) and model.

The target label is the point. A browser run's latency describes whichever GPU
the visitor happens to own; a server run's describes one fixed card plus the
network hop to reach it. Any panel that averages them together shows a number
that is true of neither. Failures are labelled by reason rather than status code
because the reasons call for different responses: `unavailable` means the
machine is off, `timeout` means it is struggling, `upstream` means it refused us.

Browser figures are recorded from what the client reports, so they are a claim
rather than our own measurement — another reason not to merge the two.

## Setup

1. **Pick a token and give it to the backend.** `METRICS_TOKEN` in the backend's
   environment (Render dashboard, or `.env` locally). At least 32 bytes.

   Without it, production does not serve `/metrics` at all. That is deliberate:
   the exposition maps every route, its traffic and its latency, which is
   reconnaissance to hand out for free. The endpoint disappears rather than the
   process refusing to boot, so forgetting the variable costs you dashboards,
   not the service.

2. **Give Prometheus the same token, as a file:**

   ```bash
   printf '%s' 'the-same-token' > prometheus/metrics-token
   ```

   `printf`, not `echo` — a trailing newline makes the token wrong and every
   scrape 401s. Prometheus does no environment expansion in its config, which is
   why this is a file and not a variable.

3. **Configure Grafana and start:**

   ```bash
   cp .env.example .env      # set GRAFANA_PASSWORD
   docker compose up -d
   ```

4. Open <http://localhost:3001>, log in, and the dashboard is already there —
   datasource and dashboard are both provisioned from this directory rather than
   clicked in, so the next person to bring this up gets the same thing.

## Verify

Check the scrape before trusting a panel:

```bash
# Targets should be "up". mf-backend-local is expected to be down unless a
# backend is running on this machine.
open http://localhost:3001   # Connections -> Prometheus -> ... or:
docker compose exec prometheus wget -qO- http://localhost:9090/api/v1/targets \
  | grep -o '"health":"[a-z]*"'
```

Empty panels with no error almost always means the datasource uid does not
match what the dashboard references — both are pinned to `prometheus` here.

## Known adjustment points

This stack has not been run yet: it was written on a machine with Docker
stopped, and only the config files' syntax has been checked. The Go side *has*
been verified — `/metrics` was exercised locally, including that three different
run ids collapse to one series and that the token guard returns 401.

Most likely to need a fix on first run:

- the scrape of Render over the public internet (TLS, the free instance being
  asleep and answering slowly, or the token file having a stray newline)
- `host.docker.internal` for the local target, which needs the `extra_hosts`
  entry on Linux and is already set
