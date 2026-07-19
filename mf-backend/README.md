# mf-backend — Raw LLM Monitoring & Decision Scoring API

Go + Postgres backend for the MasterFabric capstone. Records LLM runs produced
in the browser (WebLLM/Gemma) and grades them with a transparent, rule-based
**decision scoring** engine.

- **Live API:** **https://mf-backend-zqsa.onrender.com** ✅ live
- **Frontend:** see [`mf-frontend`](../mf-frontend) → Vercel
- **Stack:** Go 1.26 · chi router · pgx/Postgres · JWT · bcrypt

---

## Architecture

```
cmd/server/main.go        wiring + graceful shutdown
internal/
  config/                 env config + /config, /version
  common/                 db pool, JSON, errors, CORS + JWT middleware
  auth/                   register/login/refresh + JWT + sessions (repository pattern)
  llm/                    runs, metrics + rule-based decision scoring
migrations/               embedded SQL schema (go:embed)
```

Layered, dependency-injected, repository pattern — aligned to the MasterFabric
Go curriculum (Days 36–60: config/middleware → databases/repositories → auth).

## Endpoints (21 total, ≥20 required)

| Module | Method & path | Auth |
| --- | --- | :---: |
| Config | `GET /config` | – |
| Config | `GET /version` | – |
| Common | `GET /health` | – |
| Common | `GET /ready` | – |
| Auth | `POST /auth/register` | – |
| Auth | `POST /auth/login` | – |
| Auth | `POST /auth/refresh` | – |
| Auth | `POST /auth/logout` | – |
| Auth | `GET /auth/me` | ✓ |
| Auth | `PATCH /auth/me` | ✓ |
| Auth | `POST /auth/change-password` | ✓ |
| Auth | `GET /auth/sessions` | ✓ |
| Auth | `DELETE /auth/sessions/{id}` | ✓ |
| LLM | `GET /llm/models` | – |
| LLM | `POST /llm/runs` | ✓ |
| LLM | `GET /llm/runs` | ✓ |
| LLM | `GET /llm/runs/{id}` | ✓ |
| LLM | `DELETE /llm/runs/{id}` | ✓ |
| LLM | `POST /llm/runs/{id}/score` | ✓ |
| LLM | `GET /llm/runs/{id}/score` | ✓ |
| LLM | `GET /llm/metrics` | ✓ |

## Decision scoring

A run's score (0–100, grade A–F) is a weighted blend of five transparent
dimensions, each returned in the response `breakdown`:

| Dimension | Measures |
| --- | --- |
| `completion` | Did the model actually answer (not empty / not a refusal)? |
| `latency` | Response time vs. thresholds (≤1.5s = 100, ≥20s = 0) |
| `efficiency` | Characters produced per completion token |
| `keywords` | Fraction of expected keywords present |
| `length` | Answer neither too short nor excessively long |

Weights default to a balanced profile but can be overridden per request.

## Local development

```bash
# Postgres (Homebrew)
brew services start postgresql@16
createdb mf_monitoring

cp .env.example .env        # adjust DATABASE_URL / JWT_SECRET if needed
go run ./cmd/server         # migrations run automatically on boot
# → http://localhost:8080/health
```

## Deploy (Render)

This repo ships a [`render.yaml`](./render.yaml) Blueprint that provisions the
web service **and** a managed Postgres, wiring `DATABASE_URL` automatically and
generating `JWT_SECRET`. After the first deploy, set `CORS_ORIGINS` to your
Vercel URL. A multi-stage [`Dockerfile`](./Dockerfile) is provided as an
alternative runtime.
