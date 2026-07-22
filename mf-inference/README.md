# mf-inference — self-hosted MLC-LLM

The server-side half of the LLM. The browser path (WebLLM/WebGPU) stays exactly
as it is; this adds a second target so the same model can be run — and measured —
in two places.

```
Vercel (frontend) ──> Render (Go backend) ──> Cloudflare Tunnel ──> this box
                                                                     │
                                                        cloudflared ─┤
                                                            gateway ─┤  (Caddy, X-API-Key)
                                                                mlc ─┘  (mlc_llm serve, CUDA)
```

Nothing on Render moves. The backend gains one env var pointing at the tunnel
hostname.

## Why it is not on Render

Render's free instance is 512 MB of RAM with no GPU; the Gemma 2 2B weights
alone are ~1.4 GB. Render offers no GPU on any plan, a second container would
need a paid private service, and a free service sleeps after 15 minutes —
waking it would reload the weights from scratch.

## Target hardware

Verified against: GTX 1660 Ti (6 GB), 16 GB RAM, Windows 11 Pro, driver 610.62.

The card is Turing (compute 7.5), which is the *minimum* architecture CUDA 13
still supports — Maxwell, Pascal and Volta were dropped in 13.0. One generation
older and this would not work at all.

6 GB is the real constraint. Gemma 2 2B q4f16_1 needs ~1.5 GB and leaves plenty
of headroom, but Windows is drawing the desktop on the same card, so do not
reach for 7-8B models here.

## Setup (once)

1. **WSL2 + Docker Desktop.** In Docker Desktop: Settings → General → *Use the
   WSL 2 based engine*. GPU passthrough does not work with the Hyper-V backend.

2. **Verify the GPU is visible to containers.** This is the step that catches
   a broken NVIDIA Container Toolkit install, and it is much easier to debug
   here than inside the MLC build:

   ```powershell
   docker run --rm --gpus all nvidia/cuda:12.8.1-base-ubuntu22.04 nvidia-smi
   ```

   You should see the 1660 Ti listed. If not, stop and fix this first.

3. **Configure.**

   ```powershell
   copy .env.example .env
   # fill in LLM_API_KEY (TUNNEL_TOKEN can stay empty for now)
   ```

4. **Start.**

   ```powershell
   docker compose up -d --build
   docker compose logs -f mlc
   ```

   The first start downloads ~1.5 GB of weights into the `model-cache` volume.
   Wait for the log line saying the server is listening before testing.

   This brings up `mlc` and `gateway` only. The tunnel is a separate opt-in
   profile — see below — because nothing outside this machine needs the model
   until the Render backend starts calling it.

## Exposing it (only when the backend needs to reach it)

Everything above works on this machine alone. The tunnel is what lets the Render
backend — which is in Oregon and has no route to a home network — reach the card.

Two options, and the choice comes down to whether you already have a domain on
Cloudflare.

### Quick tunnel — no account, no domain

```powershell
docker compose --profile quicktunnel up -d
docker compose --profile quicktunnel logs cloudflared-quick
```

The `--profile` flag is needed on *every* command that names the service, not
just `up`. Without it Compose leaves profiled services out of the model
entirely and reports "no such service".

Cloudflare hands out a random `https://<words>.trycloudflare.com`. It works
immediately, but **the hostname changes every restart**, so `LLM_BASE_URL` on the
backend has to be updated each time. Good enough for development and a live
demo; not something to leave running.

### Named tunnel — stable hostname, needs a domain

A public hostname must live in a Cloudflare zone, so this path requires a domain
already added to your Cloudflare account.

1. Dashboard → **Networking → Tunnels** → Create a tunnel → Cloudflared →
   Docker. Copy just the `--token` value into `TUNNEL_TOKEN` in `.env`; the rest
   of the command is what compose already does.
2. Open the tunnel → **Routes** tab → **Add route** → **Published application**.
   Set the subdomain and domain, and a Service URL of `http://gateway:8080` —
   `gateway` resolves because cloudflared shares the compose network. The
   resulting hostname is what goes into `LLM_BASE_URL`.

   (Cloudflare moved tunnel management into the main dashboard in February 2026
   and replaced the old "Public Hostname" tab with "Routes". The other route
   types there — Tunnel CIDR, Tunnel Hostname — are for private network access,
   not this.)
3. Start it:

   ```powershell
   docker compose --profile tunnel up -d
   docker compose --profile tunnel logs cloudflared
   ```

   To avoid repeating the flag, set it for the shell instead:
   `$env:COMPOSE_PROFILES="tunnel"`.

### Either way, verify before wiring the backend

From a machine that is *not* this one:

```bash
curl -s -o /dev/null -w "%{http_code}\n" https://<hostname>/health          # 200
curl -s -o /dev/null -w "%{http_code}\n" -X POST https://<hostname>/v1/chat/completions \
  -H "Content-Type: application/json" -d '{}'                              # 401
```

The second one must be 401. If a tunnel is up and that request succeeds, the
card is open to anyone who guesses the hostname.

## Verify

Locally on the Windows box:

```powershell
curl.exe http://127.0.0.1:8080/health

curl.exe -X POST http://127.0.0.1:8080/v1/chat/completions `
  -H "Content-Type: application/json" `
  -H "X-API-Key: <your LLM_API_KEY>" `
  -d '{\"model\":\"gemma-2-2b-it-q4f16_1-MLC\",\"messages\":[{\"role\":\"user\",\"content\":\"Explain what a goroutine is in one paragraph.\"}]}'
```

Then the same request against the tunnel hostname from anywhere. Check that
**omitting** `X-API-Key` returns 401 — if it returns a completion, the GPU is
open to the internet.

Record how long the request takes. That number decides the backend's
`LLM_TIMEOUT`, and whether the synchronous request/response design holds at all
or the endpoint has to stream.

## Scaling out

The gateway load balances across however many `mlc` replicas exist:

```powershell
# Warm the cache with one replica first — see below.
docker compose up -d
# ...make one request, then:
docker compose up -d --scale mlc=2
```

Caddy re-resolves the service name every 10s, so replicas are picked up while
running, and `round_robin` sends each request to the next one in turn.

**A replica joins the rotation before it can serve.** The Caddyfile configures
active health checks, but they do not apply to dynamic upstreams — a replica
that started seconds ago and is still loading 1.5 GB of weights stays in
rotation and answers 502 instantly. This was measured: scaling up and testing
immediately produced two instant 502s and one success.

Passive checks do catch it — two failures park a replica for 30s — so the
system recovers on its own, at the cost of a few failed requests. Wait for
`/v1/models` to answer 200 on each replica before sending real traffic;
`check-lb.ps1` does this before it measures anything.

`least_conn` would be the better policy on paper, since generation times vary by
an order of magnitude. It was tried first and measurably did not work here:
every request went to the first replica, including requests that overlapped for
three seconds. It compares a per-upstream count of in-flight requests, and the
dynamic-a module rebuilds its upstream list as it resolves, so those counts are
not the ones that accumulated.

**What this buys, honestly: concurrency, not throughput.**

All replicas share one GTX 1660 Ti. Each loads its own copy of the weights
(~1.5 GB) and they contend for the same SMs, so two replicas do not answer twice
as fast — under sustained load they may be slightly slower than one, because the
card now context-switches. What changes is that a long request no longer blocks
a short one behind it in a single queue.

Real throughput scaling for GPU inference needs more GPUs. That is a property of
the hardware, not of this configuration, and it is worth stating plainly rather
than presenting a replica count as a performance result.

Practical limits on this box:

- **VRAM is the ceiling.** 6 GB total, some of it already drawing the Windows
  desktop. Two replicas of Gemma 2 2B q4 fit; three probably do not. If a
  replica dies on startup with an out-of-memory error, that is the limit found.
- **Warm the cache first.** MLC compiles CUDA kernels on a cold start and writes
  them to the shared volume. Starting several replicas against an empty cache
  has them all compiling simultaneously, which is slow and wasteful.
- **Watch it in Grafana.** `sum by (target) (rate(llm_generation_tokens_total…))`
  and the latency histogram in `mf-observability/` are what show whether adding a
  replica actually changed anything — which is the interesting question, and the
  answer may well be "no".

## Known adjustment points

- **Wheel tag.** MLC publishes one wheel per CUDA minor version and the set
  changes over time. If the build fails on the `pip install`, check
  <https://llm.mlc.ai/docs/install/mlc_llm.html> for the currently published
  tags, then set `CUDA_TAG` in `.env` and the matching
  `nvidia/cuda:<version>-devel-ubuntu22.04` base in `mlc/Dockerfile`. The host
  driver does not need to match — it is newer than any of these and backward
  compatible.

  Read the comments in `mlc/Dockerfile` before changing anything there. Several
  plausible-looking choices fail in ways that do not announce themselves: the
  `runtime` base image is missing `nvcc` and the container restart-loops, the
  `-nightly-` package variant is stale, an unpublished tag silently resolves to
  an empty PyPI placeholder, and `apache-tvm-ffi` newer than 0.1.11 breaks the
  compiled extension. Each of those cost a debugging session already.
- **Out of memory.** Drop to `Llama-3.2-1B-Instruct-q4f16_1-MLC`, or close
  whatever else is using the card.
- **The box must stay awake.** Sleep drops the tunnel and the backend starts
  returning 502. Disable sleep in Windows power settings.

## Security notes

- `mlc` publishes no host port; the only route in is through the gateway.
- The gateway rejects any request without `X-API-Key`. Caddy has no built-in
  rate limiting, so a leaked key means unmetered use of the card — rotate it by
  changing `.env` on this side and `LLM_API_KEY` on the backend.
- `.env` holds both the tunnel token and the shared secret. It is gitignored;
  keep it that way.
