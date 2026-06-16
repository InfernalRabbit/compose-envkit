---
name: provenance-b-lite-service-only-scope
description: cenvkit v2 B-lite Effects only cover services.<name>.<field>; top-level networks/volumes/configs/secrets/x-* ${VAR} refs are silently dropped (probe-confirmed)
metadata:
  type: project
---

cenvkit v2 rich-provenance B-lite emits `VarTrace.Effects` ONLY for paths under
`services.<name>.<field>`. `splitServiceField` (plan
`2026-06-16-cenvkit-rich-provenance.md:556-567`) returns ok=false for any path
not prefixed `services.`, and walkDict's callback (`:489-491`) early-returns on
that, so `${VAR}` references in top-level `networks:`, `volumes:`, `configs:`,
`secrets:`, and `x-*` blocks are silently excluded from Effects.

**Why:** probe-confirmed (live compose-go v2.11.0, /tmp/cgprobe) — the raw
non-interpolated dict walk DOES visit and surface these paths:
`networks.frontnet.driver`, `networks.frontnet.driver_opts.parent`,
`volumes.data.driver_opts.type`, `x-defaults.logging.driver` all carry live
`${VAR}` leaves. They are dropped at splitServiceField, not at the walk. A var
used ONLY in such a block (and not a chain key) yields empty Effects AND empty A
→ user concludes "unused" when it is in fact interpolated.

**How to apply:** This is a documented-scope gap, not a correctness bug — the
service-only narrowing is a defensible B-lite increment. Treat findings here as
DOC severity (spec §2 B-lite + §8 non-goals + a code comment on
splitServiceField marking the skip intentional), NOT as a blocking defect. Same
class as [[carried-bug-classes-cenvkit]] discipline of stating narrowings
explicitly. Watch the inverse risk too: if a later increment widens scope, the
guard/test must assert top-level refs newly appear.
