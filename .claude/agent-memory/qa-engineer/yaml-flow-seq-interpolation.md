---
name: yaml-flow-seq-interpolation
description: compose-go rejects ${VAR} inside YAML flow sequences — use block form for env_file paths with tokens
metadata:
  type: feedback
---

`env_file: [./${VAR}/.b.env]` (flow sequence) causes a YAML parse error in compose-go:
"yaml: while parsing a flow sequence ... did not find expected ',' or ']'"

The `${` confuses the YAML flow sequence parser before interpolation can occur.

**Why:** YAML flow sequences use `{` as a delimiter; `${` looks like a nested mapping start, breaking the parser.

**How to apply:** Any `env_file:` path containing a `${VAR}` token must use block sequence form:
```yaml
env_file:
  - path: ./${VAR}/.b.env
    required: false
```
Never flow-sequence form when the path contains `${`.
