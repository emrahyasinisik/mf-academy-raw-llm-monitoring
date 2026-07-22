# mf-observability — Prometheus, Loki and Grafana

Metrics and logs, in their own containers.

```
mf-backend      <--scrape--  prometheus  <--query--\
  /metrics                      :9090               \
                                                     >--  grafana  :3001
docker containers --read-->  alloy --push--> loki   /
  (mlc-1, mlc-2, gateway, …)                :3100  /
```

Two directions, and both are worth getting right because they are commonly drawn
backwards:

- **Metrics are pulled.** The application never pushes anywhere; Prometheus
  scrapes on a schedule. If this whole stack is down, the backend neither knows
  nor cares.
- **Logs are pushed** — but by Alloy, not by the applications. Containers keep
  writing to stdout exactly as before and know nothing about Loki; Alloy reads
  their logs off the Docker socket.

Grafana talks only to Prometheus and Loki. It never touches the backend.

Alloy rather than Promtail: Promtail reached end of life on 2 March 2026.

## Why logs as well as metrics

Metrics say a replica is slow. Logs say why.

That matters most since the inference service became scalable: with `mlc-1` and
`mlc-2` both running, "which replica served this, and what did it do" is a
question metrics can only answer in aggregate. The `container` label separates
them, and the `service` label groups them back together.

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

## Reading the logs

In Grafana, the dashboard's bottom row has them. To explore, use **Explore** with
the Loki datasource:

```logql
{service="mlc"}                            # every inference replica
{container="mf-inference-mlc-2"}           # one replica
{service="gateway"} |= "401"               # rejected requests at the gateway
{project="mf-inference"}                   # the whole inference stack
sum by (container) (rate({service="mlc"}[1m]))   # which replica is busy
```

That last one is the cheapest answer to "is the load balancer really using both
replicas" — a flat line at zero for one container means it is up and idle.

## Known adjustment points

Prometheus and Grafana have been run and verified. **Loki and Alloy have not** —
they were added on a machine with Docker stopped, so only the config syntax is
checked.

Most likely to need a fix on first run:

- **The Docker socket mount.** Alloy needs `/var/run/docker.sock`, which works
  with Docker Desktop's WSL2 backend on Windows. If Alloy logs a permission or
  connection error against the socket, that is the cause.
- **No logs appearing at all.** Check Alloy first (`docker compose logs alloy`);
  it reports discovery and push failures plainly. Its own UI is not exposed
  here, so the logs are the diagnostic.
- The scrape of Render over the public internet (TLS, the free instance being
  asleep, or the token file having a stray newline).
- `host.docker.internal` for the local target, which needs the `extra_hosts`
  entry on Linux and is already set.
