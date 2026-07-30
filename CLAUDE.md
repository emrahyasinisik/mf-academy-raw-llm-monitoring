# CLAUDE.md

## What this is

A case goes in, a rubric-scored report comes out — on the operator's own
hardware. The model does not score. It fills in a rubric: one rating plus
supporting quotes per criterion. The weighted total is computed
deterministically in Go (`mf-backend/internal/analysis/scoring.go`).

That split is the product, not an implementation detail. "Why should I trust an
LLM's investment score?" — you don't. You trust the rubric, you audit the
evidence the model collected, and the arithmetic is open and ours. A rejection
becomes defensible: not "the AI said no" but "criterion 4 scored 2 of 5, because
of this figure on page 12 of the deck."

Two domains ship: **startup investability** and **digital marketing channel
mix**. Psychological assessment is deliberately out of scope — diagnosis is
regulated and beyond this hardware.

## Current phase: go-to-market

The engine works. The remaining work is selling it and raising on it, so
default to that when a task is ambiguous.

**Priorities, in order** — these are `docs/urun-ve-pazarlama.md` §7, and they
gate outreach. Contact made before they are done is contact burned:

1. One flawless sample report (an anonymised real deck → full analysis). This
   is the entire sales asset.
2. ~~The rubric published and readable.~~ Done — both rubrics, with weights, are
   in `README.md`; they seed from `migrations/005_analysis.sql`. Keep them in
   sync when a weight changes. A closed rubric contradicts the pitch.
3. A consistency number. The machinery is built, not missing: `POST
   /analysis/trial` reruns a case (5 default, 10 max) and reports the spread,
   `GET /analysis/trials/{group}` fetches it, and `PerCriterionStdDev` localises
   an unstable row. What is missing is a *published figure* — run it on the
   sample case and put the number in the sales material.
4. Install under 15 minutes. One `docker compose up`.
5. First 60 seconds: sample corpus and sample case, not an empty screen.

**Do not** start new architecture in this phase. In particular, do not add
autoscaling or more MLC replicas — §13 of `docs/nasil-calisiyor.md` measured it:
two replicas load the weights twice (5.4 / 6.1 GB) and leave no room for
batching on a 6 GB card. The right axis is in-process batching, and it is not
this phase's problem.

The codebase is itself a marketing channel. Comments here explain *why*,
including the attempts that failed. Keep writing them that way — converting
engineering notes into content is zero extra work, and it is channel #4.

The backend already serves MCP (`internal/mcp`: `list_analysis_domains`,
`analyze_case`, `get_report`, `list_reports`, `search_wiki`, `ask_wiki`). That
is channel #2 — free distribution to the Claude/Cursor audience — and it is
built. Publishing it to a directory is a go-to-market task, not a coding one.

## Repo map

| Path | What | Runs on |
|---|---|---|
| `mf-backend/` | Go 1.26.5 API. `analysis` = the product, `decision` = the live-researching investability persona, `mcp` = MCP server, plus `wiki`, `auth`, `llm`, `admin`, `obs`, `settings` | Render |
| `mf-frontend/` | Next.js 16 SPA, Tailwind, in-browser WebLLM | Vercel |
| `mf-inference/` | MLC + llama.cpp + Caddy gateway + Cloudflare tunnel, and the `peft/` training pipeline | Emrah's Windows box, GTX 1660 Ti |
| `mf-observability/` | Prometheus, Loki, Alloy, Grafana | Same Windows box |

Render can never host inference — it has no GPU. `LLM_BASE_URL` unset is a
supported state: `POST /llm/generate` answers 503 and the browser path is
unaffected, so the API survives the inference machine being switched off.

## Commands

```bash
# backend
cd mf-backend && go run ./cmd/server        # :8080, migrates on boot
go test ./...
go build -o app ./cmd/server                # what Render runs

# frontend
cd mf-frontend && npm run dev               # :3000
npm run build && npm run lint

# inference / observability (on the Windows box, not the Mac)
docker compose -f mf-inference/docker-compose.yml up -d
docker compose -f mf-observability/docker-compose.yml up -d
```

Training runs on **Kaggle**, not the GPU box: kernels `emrahik/rubric-qlora`
(train) and `emrahik/rubric-eval` (eval, sourced from the train kernel), both
T4, both fed by dataset `emrahik/rubric-dataset`. See
`mf-inference/peft/kaggle/README.md` and `push.sh`.

## Positioning language

The frontend and any outward copy must not say:

- ❌ "AI makes the investment decision" — nobody buys a machine that takes their
  liability. What is sold is **consistency in first-pass screening**.
- ❌ "best model" — a 2–4B model on a 6 GB card loses that fight. The axes are
  rubric transparency, data sovereignty, consistency.
- ❌ "automatic" — human-in-the-loop is the point. The buyer wants to be faster,
  not replaced.

Describe the product, never the hardware it happens to run on. No workshop copy
in the UI — the card, the tunnel, and the training run are our problems, not the
user's. The UI is dark-only by choice.

## Traps that already cost us time

- **`mlc_llm` does not validate the request's `model` field.** It answers from
  the single model it loaded and echoes back whatever id you asked for. A wrong
  id is not an error — one model answers while the records and dashboards label
  it as another.
- **A child context cannot extend its parent's deadline (Go).** The global 5s
  timeout on the root router made the 25s route timeout dead code; requests died
  at exactly 5001ms. Apply short limits to subtrees, keep `/llm` out of them.
- **`least_conn` does not work behind Caddy's `dynamic a`.** The upstream list
  is regenerated constantly so the counters never accumulate. We use
  `round_robin`. Active health checks are also inoperative on dynamic upstreams;
  passive `fail_duration` is what recovers a still-loading replica.
- **Prometheus cardinality.** Label with the chi *route pattern*, never the raw
  path; unmatched requests go to `route="unmatched"`. A run id as a label is a
  million series and a dead Prometheus.
- **Promtail is retired (2 Mar 2026).** Alloy replaced it.
- **Alloy reaches the Docker socket read-only.** `:ro` is not cosmetic — a
  writable socket is control of the whole machine.
- **Latency by target is not one number.** Browser runs measure the visitor's
  card; server runs measure ours plus network. Averaging them describes neither
  — hence the `target` column and `by_target` in `GET /llm/metrics`.
- **Two metadata traps in the Kaggle pipeline cost two runs.** The runbook has
  them; read it before pushing a kernel.
- **Judge the adapter on `clean`.** The base model already follows evidence, so
  the older eval metric hits a ceiling and shows no adapter gain that is real.

## Deploy

- Backend → Render, frontend → Vercel, both from this repo.
- `render.yaml` sets `autoDeploy: true`, **but the GitHub webhook does not fire
  in practice.** A push is half a release; trigger the deploy explicitly and
  verify it, and never report a change as shipped on the strength of a push.
- For a breaking contract change, work out which side must land first from **the
  fields each side reads on the other's response** — not from which service owns
  the change. Frontend and backend build at different speeds, so there is always
  a window where they disagree.
- Branches are never deleted, merged or not. Do not offer cleanup.
- Secrets stay out of the repo: `LLM_API_KEY` is `sync: false` and lives in the
  Render dashboard.

## Where things are written down

- `docs/urun-ve-pazarlama.md` — what this sells and to whom. ICP, positioning,
  pricing, channels, and an honest list of the plan's weak points. Read it
  before any product or copy decision.
- `docs/nasil-calisiyor.md` — the architecture tour, including the measurements
  and what we deliberately did not build.
- `docs/peft-nedir.md` — fine-tuning.
- `mf-inference/peft/PERSONA_RUNBOOK.md`, `peft/kaggle/DATASHEET.md` — training
  and dataset procedure.

The root `README.md` still describes the earlier Gemma / decision-scoring
capstone framing and predates the investability pivot. Treat it as stale, and
fix it before the repo is used as a sales asset — it is the first thing a
prospect reads.
