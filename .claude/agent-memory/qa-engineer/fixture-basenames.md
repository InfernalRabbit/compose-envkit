---
name: fixture-basenames
description: Confirmed Layer-2 env file basenames in examples/monorepo on disk (as of 2026-06-15)
metadata:
  type: project
---

Verified on disk at `examples/monorepo/`:
- `web/.web.env` — web service base env (WEB_PORT=18080)
- `web/.web.dev.env` — web dev tier
- `web/.web.prod.env` — web prod tier
- `api/.api.env` — api service base env (API_PORT=19090)
- `services/reports/.reports.env` — deep nesting (services/<svc>/)

Root Layer-1 files ABSENT (only `example.*` exist): `.env`, `.dev.env`, `.prod.env`, `.secrets.env` do NOT exist at the root — supply `Env: []string{"COMPOSE_ENV=dev"}` directly in engine unit tests rather than relying on root Layer-1 files.

**Why:** Tests against the monorepo fixture that rely on root `.env` existing will fail. The fixture only has `example.env`, `example.dev.env`, `example.prod.env`.

**How to apply:** In `TestResolve_MonorepoFixture_CrossSubproject`, pass `Env: []string{"COMPOSE_ENV=dev"}` explicitly; do not seed a `.env` file in the monorepo fixture dir.
