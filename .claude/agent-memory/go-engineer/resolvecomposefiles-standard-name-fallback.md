---
name: resolvecomposefiles-standard-name-fallback
description: engine resolveComposeFiles must do standard-name discovery when COMPOSE_FILE is unset — qa's discover_test pins len()>0 for a bare compose.yaml
metadata:
  type: feedback
---

`internal/engine/discover.go`'s `resolveComposeFiles(dir, env)` must return the
existing standard compose files (compose.yaml/.yml, docker-compose.yaml/.yml)
when `COMPOSE_FILE` is UNSET — NOT `nil`.

**Why:** The plan's Task 3 Step 5 sketch returned `nil` when COMPOSE_FILE was
empty (delegating standard-name discovery to the gate only). But qa's binding
test `internal/engine/discover_test.go::TestHasComposeFile` case
"unset + standard name present" asserts `len(resolveComposeFiles(dir, env)) > 0`
== true for a bare `compose.yaml` with no COMPOSE_FILE in env. The test is the
contract I cannot edit; the plan sketch was the loser. Folding the standard-name
fallback INTO `resolveComposeFiles` (instead of only the gate) also makes the
resolver a complete "what config files exist?" answer and keeps gate+loader from
drifting (HasComposeFileEnv is just `len(resolveComposeFiles)>0`).

**How to apply:** When implementing against this team's plans, the qa RED test on
disk is the binding contract — re-read it before coding and reconcile any plan/test
divergence in the test's favor, then flag the divergence to team-lead. Don't blindly
transcribe a plan code-sketch. See [[d1-lenient-enumeration-lever]] for the engine's
other load-time contract. Verified GREEN at compose-go v2.11.0, all 4 engine_test
cases + 8 discover_test cases pass.
