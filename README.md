# keepup

[![ci](https://github.com/quike/keepup/actions/workflows/ci.yml/badge.svg)](https://github.com/quike/keepup/actions/workflows/ci.yml)
![github-release](https://img.shields.io/github/v/release/quike/keepup)
[![codecov](https://codecov.io/gh/quike/keepup/graph/badge.svg?token=IAD5CBIVTY)](https://codecov.io/gh/quike/keepup)
[![go-score](https://goreportcard.com/badge/github.com/quike/keepup)](https://goreportcard.com/report/github.com/quike/keepup)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Simple task runner.

keepup runs YAML-declared **groups** of commands composed into named **flows**.
Groups are atomic and reusable; flows decide how they run — either as ordered
parallel waves (**step mode**) or scheduled automatically from the data
dependencies between them (**dag mode**). Data flows between groups through
`{{ output "name" }}` references — rendered by a Go-template + sprig engine, and
validated up front. A single binary, no runtime, safe-by-default execution (no
shell unless you ask for it), and incremental re-runs via content-based caching.

## Features

| Feature                  | What it gives you                                                                                                           |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------- |
| **Groups + flows**       | Reusable command units composed into many named pipelines in one file — no duplication.                                     |
| **Two scheduling modes** | `step` (explicit parallel waves with barriers) or `dag` (topological, inferred from `{{ output.X }}` data deps).            |
| **Output piping**        | Pass one group's captured stdout into another via `{{ output "name" }}`, validated at load time.                            |
| **Templating**           | `command`/`params` are Go templates with [sprig](https://masterminds.github.io/sprig/): `{{ output "sha" \| trunc 7 }}`, `{{ env "CI" \| default "local" }}`. Legacy `{{ output.X }}` still works. |
| **Caching**              | Per-group `cache: { reads, writes }` fingerprints inputs (hash or mtime) and skips unchanged work, replaying stored output. |
| **Watch mode**           | `keepup watch` re-runs a flow on file changes (using each group's `cache.reads`); caching makes unaffected groups no-ops.   |
| **Gating**               | `require:` (must pass) and `skip-if:` (skip when already done) predicates short-circuit a group before it runs.             |
| **Timeouts & retries**   | Per-step / per-flow `timeout` and `retries` envelope around each command attempt, with backoff.                             |
| **Safe execution**       | Commands run as real argv by default (no shell injection); opt into a shell per group with `shell:`.                        |
| **Env layering**         | Global `env:` plus per-group overrides, merged over the process environment.                                                |
| **Discoverability**      | `keepup list`, `keepup validate`, and `keepup graph` (Mermaid diagram of the data DAG).                                     |
| **Migration**            | `keepup migrate` converts legacy v1 configs to v2 and validates the result.                                                 |

## Quick start

```sh
keepup run            # run the default flow
keepup run ci         # run a named flow
keepup watch dev      # re-run on file changes
keepup list           # show flows; `list groups` shows groups
keepup graph ci       # Mermaid diagram of the data DAG
```

## Documentation

- [Index](docs/index.md)
- [Usage](docs/USAGE.md) — quick tour of the CLI
- [Configuration](docs/CONFIG.md) — full schema reference
- [FAQ](docs/FAQ.md) — flows, modes, caching, migration
