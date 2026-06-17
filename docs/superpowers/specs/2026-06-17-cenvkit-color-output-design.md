# cenvkit — colored / formatted output (lipgloss)

Status: **design, 2026-06-17 (owner-approved direction; implementation
plan-gated).** Presentation-layer feature; changes no behavior contract, no
`Report` model, no compose-go usage. Additive.

## 1. Motivation

cenvkit's human output is flat, uncolored text. The removed POSIX-`sh` kit had a
colored, formatted overview (green `+`, yellow `~`, dim `·`, service markers,
bold headers). The owner wants that visual fidelity back — **rich (TUI-style)
coloring across ALL human outputs** — without sacrificing the pipe-friendly,
scriptable, daemon-free design.

## 2. Decisions (owner-confirmed)

- **Library: `github.com/charmbracelet/lipgloss`** (NOT Bubbletea). Lipgloss is a
  *styling* library that renders styles to **strings** (`style.Render(s)` →
  ordinary `fmt.Println`) — non-interactive, pipe-safe. Bubbletea (interactive
  TUI runtime, terminal takeover) was explicitly rejected: it would break
  `cenvkit env-files | …`, `--json`-to-CI, and the print-and-exit model. Lipgloss
  uses `termenv` under the hood for color-profile detection (TrueColor/256/16/
  NoColor), terminal light/dark background, and NO_COLOR/non-TTY auto-degrade.
- **Intensity: rich (TUI-style)** — color the structural + semantic elements
  vividly (markers, keys, values, paths, service names, headers, source labels),
  not just a couple of markers. Keep values readable (see §4).
- **Control: standard.** Color is AUTO when stdout is a TTY; off when piped/
  redirected. Honor `NO_COLOR` (disable) and `CLICOLOR_FORCE`/`FORCE_COLOR`
  (force). A `--color=auto|always|never` flag overrides all. **`--json` / machine
  output is NEVER colored** regardless of the above.
- **Owner relaxed the "zero-dep / thin" guidance for this feature** — a modest,
  well-maintained dep tree (lipgloss → termenv, go-isatty, go-colorful) is
  accepted for the quality it buys. (CLAUDE.md "thin" still governs elsewhere.)

## 3. Architecture (seam intact)

- **New `internal/style`** (wraps lipgloss; pure presentation, NO compose-go).
  Defines the palette (§4) as lipgloss styles and exposes a small **`Styler`**
  with semantic methods — e.g. `Header(s)`, `MarkerNew()/MarkerOverride()/
  MarkerRepeat()`, `Key(s)`, `Value(s)`, `Path(s)`, `Service(s)`, `SourceLabel(s)`,
  `Gap(s)`, `Ok(s)`/`Fail(s)`/`Created(s)`/`Skipped(s)`, `ErrorMsg(s)` — each
  returning a rendered string. A **disabled** Styler (profile `Ascii`/NoColor)
  returns plain text, so every call site is colorblind-safe by construction.
- **Injection, not a global.** `cmd/cenvkit` resolves ONE `Styler` at startup
  from the color decision and injects it: into `provenance` render via
  `HumanOpts.Style`, and uses it directly for `env-files`/`validate`/`init`/error
  output. The **`--json` path always uses a disabled Styler**. (A nil/zero Styler
  ⇒ plain, so existing render tests that construct `HumanOpts` without a Styler
  stay green unchanged.)
- **Layering to keep `internal/provenance` light:** `provenance` depends on a
  minimal `Styler` **interface** (or a struct of string→string funcs) it can be
  handed; the concrete lipgloss-backed implementation lives in `internal/style`
  and is wired by `cmd`. (Plan picks interface-in-provenance vs
  provenance-imports-style; either keeps the lipgloss dep out of the hot test
  path and lets render tests use a fake/disabled styler.)
- **Seam:** the compose-go seam rule ("only `internal/engine` imports compose-go")
  is UNAFFECTED — lipgloss is unrelated to compose-go. `internal/engine` does not
  need styling (it returns data, not output). `internal/style` imports lipgloss;
  `internal/provenance` + `cmd` use the Styler. No compose-go anywhere new.
- **Resolution helper** in `internal/style`: `Resolve(flag string, out *os.File)
  (enabled bool / profile)` computing the decision from
  `--color` → `CLICOLOR_FORCE`/`FORCE_COLOR` → `NO_COLOR` → isatty(out), and
  building a lipgloss `Renderer` with the right profile (forced Ascii when
  disabled). Exact lipgloss/termenv API is a plan-time probe (§7).

## 4. Palette (rich)

| Element | Style |
|---|---|
| Section headers (`env overview —`, `Interpolation chain`, `Runtime-only`) | bold cyan |
| Legend `+ new   ~ override   · repeat` | dim (each marker in its own color) |
| Header `COMPOSE_ENV = <v>` / `(from <src>)` | value bold green / source dim |
| Marker `+` (new) | green |
| Marker `~` (override), `old → new` | yellow marker; `old` dim, arrow dim, `new` normal |
| Marker `·` (repeat) | dim |
| Keys (`KEY =`) | bold |
| Values | normal (readable); env_file path values, see Path |
| File paths | cyan |
| Service names (`web:`) | bold magenta |
| Source labels (`(layer1)`, `(env_file)`, `(environment)`) | dim |
| `⚠ gap:` line | red; the variable name bold red |
| `validate` ok / fail | green / red |
| `init` created / skipped | DEFERRED — init is currently silent (see §6) |
| Error messages (stderr) | red |
| `version` | plain (optionally bold name) |

Background legibility: use a **fixed** ANSI-16/256 palette chosen to read on BOTH
light and dark terminals — do **NOT** use `lipgloss.AdaptiveColor` (plan probe
correction (a): adaptive resolution issues a *blocking OSC background-color query*
to the terminal, a hang/garbage risk in a print-and-exit CLI). The SEMANTIC
mapping above is the contract; concrete values finalized in the plan.

## 5. Control surface (precedence, highest first)

0. **`--json` / machine output → ALWAYS disabled.** This is the top rule and
   overrides everything below, including `--color=always`. JSON is never colored.
1. `--color=never` → disabled. `--color=always` → forced on (even when piped).
   `--color=auto` (default) → steps 2+.
2. `NO_COLOR` set (any value) → disabled.
3. `CLICOLOR_FORCE`/`FORCE_COLOR` set → forced on.
4. else: enabled iff stdout is a TTY.

`--color` is a persistent flag (applies to every subcommand). Note: `env-files`
prints paths and is often captured — TTY-gating makes coloring it safe (a pipe
gets plain), so it IS colored on a TTY per the rich decision.

## 6. Scope (all human outputs)

- **env-debug**: `--overview`, `--effective`, `--trace`, `--chain`, `--files`
  (the `provenance` render). `--json` for any of these → plain.
- **env-files**: paths in cyan (TTY-gated).
- **validate**: ok/fail messaging colored; exit codes unchanged.
- **init**: **DEFERRED** (decision 2026-06-17). `bootstrap.Init` currently prints
  nothing (silently seeds) — there is no existing output to color, and adding
  created/skipped lines is a NEW observable behavior (a separable feature), not
  styling. Out of scope for this coloring milestone; a follow-up can add init
  reporting (with its own qa + acceptance) and color it then. Leave `init` silent.
- **errors**: red, to stderr (gated on stderr TTY).
- **version**: plain (or bold name).

## 7. Risks / plan-time verification (verify-before-claim)

- **Lipgloss/termenv profile control (MUST probe).** The plan MUST verify against
  the actual lipgloss version (add to go.mod, `go doc` / context7) how to: (a)
  force NoColor/Ascii for the disabled + `--json` paths; (b) honor the precedence
  in §5 (lipgloss/termenv already auto-handle NO_COLOR + non-TTY, but the
  `--color=always` force and the `--json`-forced-plain need explicit profile
  control — likely a per-`lipgloss.NewRenderer(out)` profile set, or
  `termenv`-level). Do NOT assume the API; cite the probe.
- **Dependency add.** go.mod/go.sum gain lipgloss + transitives — go-engineer
  zone; `go mod tidy`; the CI seam check still passes (no compose-go leak); verify
  `go build`/`vet` clean and binary size/startup acceptable.
- **Existing tests stay green.** A disabled/zero Styler ⇒ plain output, so current
  provenance render tests + the 78 acceptance assertions must pass UNCHANGED.
  Acceptance/`--json` paths must remain ANSI-free (the suite greps literal
  strings; a leaked escape would break them — this is also a guard).
- **Windows / CI**: lipgloss/termenv handle this, but the acceptance matrix
  (ubuntu+macos) must stay green; CI is non-TTY so output is naturally plain
  there.
- **Not an engine-contract change** → lighter gate than v3/overview, but the plan
  still probes the lipgloss API and the architect approves before code.

## 7a. Plan-review decisions (architect sign-off, 2026-06-17)

Probe done (`go get` of lipgloss, no .go edits). Plan:
`.claude/artifacts/2026-06-17-color-output-plan.md`. **Feasibility: GREEN, additive,
no engine-contract change.** Pinned: lipgloss v1.1.0, termenv v0.16.0, go-isatty
v0.0.20, go-colorful v1.2.0 (+ transitives; golang.org/x/sys bumped 0.5.0→0.30.0).

- **Profile control (the load-bearing risk) — VERIFIED per-renderer, no global
  mutation.** `lipgloss.NewRenderer(out)` + `Renderer.SetColorProfile(profile)`
  sets a per-renderer explicit profile. AUTO (`ColorProfile()`/termenv
  `EnvColorProfile`) already honors NO_COLOR→Ascii, CLICOLOR_FORCE→force, else
  TTY-gate (and CI-set ⇒ non-TTY ⇒ plain) for free. The `--json`/disabled path
  uses a SEPARATE `Disabled()` renderer (Ascii) — never mutates the human one. The
  global `lipgloss.SetColorProfile` is NOT used.
- **Correction (a): NO `AdaptiveColor`** — fixed palette, no OSC query (see §4).
- **Correction (b): `--color=always` = `SetColorProfile(termenv.ANSI256)`** (not
  `WithTTY`, which NO_COLOR still beats). §5 updated in spirit; the top JSON rule
  and precedence stand.
- **Correction (c): CI is auto-plain** — termenv treats `CI`-set as non-TTY, so the
  acceptance matrix is plain with zero special-casing (also a no-leak guard).
- **Architecture: interface-in-provenance.** `internal/provenance` defines a
  minimal `Styler` interface + a nil-safe `plainStyler` (every method returns its
  arg) accessed via a local `st(o.Style)` helper → existing render tests (no
  `Style` field) render byte-identical plain, ZERO churn. `internal/style` is the
  ONLY package importing lipgloss; `cmd` wires the concrete `Lipgloss` impl. Seam
  intact (lipgloss ≠ compose-go; `internal/engine` untouched).
- **go.mod**: lipgloss becomes a direct dep once `internal/style` imports it; `go
  mod tidy` finalizes (the probe left it `// indirect`). Measure binary-size delta
  in impl; expected modest.

## 8. Testing

- **`internal/style` unit:** `Resolve` precedence matrix (`--color` × `NO_COLOR` ×
  `CLICOLOR_FORCE` × tty/non-tty); a disabled Styler returns byte-identical plain
  text; an enabled Styler wraps with ANSI (assert escape presence). Pure Go.
- **provenance render:** existing tests pass unchanged (disabled styler). ADD a
  few with an ENABLED (forced) styler asserting ANSI around markers / headers /
  gap — and a test that `--json` rendering carries NO ANSI even with color on.
- **acceptance:** unchanged (78); they run non-TTY / SMOKE so output is plain — a
  guard that nothing leaks ANSI into captured output. Optionally one assertion
  forcing `--color=always` then asserting an escape appears in `--overview`.
- **No-color guards:** `NO_COLOR=1` and piped (`| cat`) → no ANSI; `--json` → no
  ANSI; `--color=never` → no ANSI.

## 9. Non-goals

- **No interactive TUI** (no Bubbletea, no alt-screen, no live updates, no
  prompts) — cenvkit stays print-and-exit / pipe-friendly.
- **No user-configurable theme file / custom palette config** — one tasteful
  built-in palette (adaptive light/dark). Revisit only on demand.
- No change to any command's behavior, exit codes, `--json` schema, or the env
  resolution. Purely how human output looks.
- No coloring of machine output (`--json`, `env-files` when piped).
