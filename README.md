
# MasterFabric Capstone — Raw LLM Monitoring & Decision Scoring

<!-- GitHub strips <iframe>, so the badge is a linked <img> (both are allowed by GitHub's sanitizer). -->
<a href="https://academy.masterfabric.co">
  <img src="https://academy.masterfabric.co/academy-badge.png" alt="MasterFabric Academy" width="220">
</a>

Full-stack monorepo. A **Gemma** model runs entirely in the browser via
**WebLLM / MLC-LLM**; every run is recorded by a Go API and graded by a
transparent, rule-based **decision scoring** engine.

| | |
| --- | --- |
| **Frontend** (`mf-frontend/`) | Next.js 16 SPA → **Vercel** |
| **Backend** (`mf-backend/`) | Go 1.26 + Postgres → **Render** |
| **Live app** | **https://mf-academy-raw-llm-monitoring-ten.vercel.app** ✅ live |
| **Live API** | **https://mf-backend-zqsa.onrender.com** ✅ live |

## Repository layout

```
mf-capstone/
├── render.yaml        # Render Blueprint (backend web service + Postgres)
├── mf-backend/        # Go API — 21 endpoints, JWT auth, rule-based scoring
└── mf-frontend/       # Next.js SPA — 3 master views, WebLLM/Gemma in-browser
```

See each package's README for details:
[mf-backend/README.md](./mf-backend/README.md) ·
[mf-frontend/README.md](./mf-frontend/README.md)

## Quick start (local)

```bash
# 1. Backend (Postgres must be running; DB auto-migrates on boot)
cd mf-backend && cp .env.example .env && go run ./cmd/server   # :8080

# 2. Frontend
cd ../mf-frontend
echo "NEXT_PUBLIC_API_URL=http://localhost:8080" > .env.local
npm install && npm run dev                                     # :3000
```

Open http://localhost:3000 in a **WebGPU browser** (Chrome/Edge 111+), register,
load Gemma in the Playground, run a prompt, and watch it get scored on the
Dashboard.

## Deployment (monorepo)

- **Backend → Render:** the root [`render.yaml`](./render.yaml) provisions the
  web service (`rootDir: mf-backend`) **and** a managed Postgres, wiring
  `DATABASE_URL` and generating `JWT_SECRET` automatically.
- **Frontend → Vercel:** set the project's **Root Directory** to `mf-frontend`
  and add `NEXT_PUBLIC_API_URL = <your Render URL>`.
- After both are live, set the backend's `CORS_ORIGINS` to the Vercel URL.

Deploys are driven via the **Render MCP**, **Vercel MCP**, and delivered through
the **MasterFabric Academy MCP**.

## Curriculum alignment (MasterFabric Go track)

This capstone practices Go roadmap phases **36–60** (config/middleware →
databases/repositories → auth & security → clean architecture) and **96–100**
(capstone & professional delivery).
