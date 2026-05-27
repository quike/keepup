# Configuration reference

The keepup config is a single YAML document. By default it's read from
`$HOME/.config/keepup/keepup.yml`; pass `-c` / `--config` to point at a
different file.

This document covers the **v2 schema**, which is the only one the current
binary understands. For migrating from v1, see [FAQ](FAQ.md).

---

## Document shape

```yaml
version: 2 # required; the only accepted value
settings: { ... } # optional global settings
env: { ... } # optional global environment variables
groups: [...] # atomic, reusable command units
default: <flow> # optional; flow to run when `keepup run` has no argument
flows: { ... } # one or more named pipelines composed of groups
```

Top-level keys at a glance:

| Key        | Type   | Required | Purpose                                                |
| ---------- | ------ | -------- | ------------------------------------------------------ |
| `version`  | int    | yes      | Must be `2`. v1 is rejected with a clear error.        |
| `settings` | map    | no       | Runtime knobs (logging, dry-run, concurrency, …).      |
| `env`      | map    | no       | Global environment variables shared by every group.    |
| `groups`   | list   | yes      | The atomic units the flows compose.                    |
| `default`  | string | no       | Name of the flow to run when none is given on the CLI. |
| `flows`    | map    | yes      | Named pipelines. At least one must be declared.        |

---

## `settings`

```yaml
settings:
  dry-run: false # bool; default false. CLI --dry-run can override.
  working-dir: /tmp # string; reserved for future per-task chdir support.
  max-concurrency: 0 # int;   0 means unbounded.
  cache-dir: .keepup-cache # string; where cache fingerprints are stored.
  logging:
    level: info # trace | debug | info | warn | error
    pretty: true # true = human; false = JSON lines.
```

| Field             | Default         | Notes                                                                                                                         |
| ----------------- | --------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `dry-run`         | `false`         | When true, the runner is bypassed for every group. The CLI `--dry-run` flag also forces this on regardless of the file value. |
| `working-dir`     | `""`            | Currently reserved. Not consumed by the runner yet.                                                                           |
| `max-concurrency` | `0` (unbounded) | Caps the number of groups running concurrently across both step- and dag-mode schedulers.                                     |
| `cache-dir`       | `.keepup-cache` | Directory where per-group cache fingerprints/outputs are stored (see [Caching](#caching)).                                    |
| `logging.level`   | `info`          | Standard severity ladder. Invalid values fall back to `info`.                                                                 |
| `logging.pretty`  | `false`         | `true` for the human renderer, `false` for one JSON object per line.                                                          |

---

## `env` — global environment

A flat string-to-string map merged into every group's environment before
per-group overrides:

```yaml
env:
  AWS_REGION: us-east-1
  LANG: en_US.UTF-8
```

Precedence (lowest → highest): process env, `env:` block, `groups[].env`.

---

## `groups`

A `group` is **an atomic, reusable command**. Groups know nothing about flows;
composition lives in flows.

```yaml
groups:
  - name: build
    description: "Compile the binary"
    command: go
    params: [build, -o, bin/keepup, ./]
    env: # optional; merged on top of global env
      CGO_ENABLED: "0"
    shell: /opt/homebrew/bin/fish # optional; opts into shell mode (see below)
```

| Field         | Type       | Required | Purpose                                                                                                                 |
| ------------- | ---------- | -------- | ----------------------------------------------------------------------------------------------------------------------- |
| `name`        | string     | yes      | Unique identifier. Referenced from flows and `{{ output.X }}`.                                                          |
| `command`     | string     | yes      | The program/argv0 to execute.                                                                                           |
| `params`      | `[]string` | no       | Arguments. Passed as a real argv list — **no shell parsing** by default.                                                |
| `env`         | map        | no       | Per-group env overrides; applied on top of the global `env`.                                                            |
| `shell`       | string     | no       | When non-empty, the named shell program runs `command + params` as a single shell-interpreted line (opt-in shell mode). |
| `description` | string     | no       | Free text used by `keepup list groups`.                                                                                 |
| `require`     | string     | no       | Predicate command; non-zero exit fails the group before it runs (see [Gating](#gating-skip-if-and-require)).            |
| `skip-if`     | string     | no       | Predicate command; exit 0 skips the group (see [Gating](#gating-skip-if-and-require)).                                  |
| `cache`       | map        | no       | Skip the group when declared inputs are unchanged (see [Caching](#caching)).                                            |

### Direct exec vs. shell mode

By default keepup spawns `command` with `params` as a real argv list — no
shell interprets the line, so spaces, `;`, `$(…)`, backticks, etc. are
treated as literal data. This is safe and matches what users typically want.

When you opt in by setting `shell:` to the path of a shell program
(e.g. `/bin/sh`, `/opt/homebrew/bin/fish`), keepup joins
`command + params` into one string and feeds it to that shell's `-c` flag.
Use this only when you actually need shell features (pipes, expansions,
`$()`, etc.).

```yaml
- name: count-lines
  command: cat *.go | wc -l # only works because shell mode is on
  shell: /bin/sh
```

On Windows, the shell flag is `/C` and the default shell falls back to
`%COMSPEC%` or `cmd.exe`.

### Referencing other groups' output

A group's `command` or any of its `params` can include
`{{ output.<other-group-name> }}`. At run time the placeholder is replaced
with the captured stdout (trimmed of surrounding whitespace) of the
referenced group:

```yaml
groups:
  - name: build
    command: echo
    params: [bin/keepup-{{ output.version }}]
  - name: version
    command: git
    params: [rev-parse, --short, HEAD]
```

Reference rules:

- The referenced group must be part of the flow being executed.
- **Step mode**: the referenced group must appear in an **earlier** step (not
  the same step). Same-step references would race.
- **DAG mode**: references determine scheduling order; cycles are rejected at
  config-load.

These rules are enforced at parse time (`keepup validate`), not at run time,
so typos and ordering bugs surface immediately.

### Gating: `skip-if` and `require`

Two optional predicates let a group decide whether it should run. Both are
shell snippets evaluated through the platform shell; only their exit status
matters.

```yaml
groups:
  - name: create-cache-dir
    command: mkdir
    params: [-p, /var/cache/app]
    require: "command -v mkdir" # exit != 0 → hard fail before running
    skip-if: "test -d /var/cache/app" # exit 0 → skip the group entirely
```

| Field     | Meaning                                   | On match                                                                                                |
| --------- | ----------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `require` | Precondition that must hold               | Non-zero exit → the group (and its flow) fails with a clear error.                                      |
| `skip-if` | Condition under which work is unnecessary | Exit 0 → the group is skipped; its output is the last cached value if `cache:` is set, otherwise empty. |

Evaluation order per group: `require` → `skip-if` → `cache` → run. In
`--dry-run`, predicates are **not** evaluated — keepup only logs what it would
do.

### Caching

A `cache:` block lets keepup skip a group when its declared inputs haven't
changed since the last successful run. On a hit, the runner is not invoked and
the previously-captured output is replayed (so downstream `{{ output.X }}`
references still resolve).

```yaml
groups:
  - name: build
    command: go
    params: [build, -o, bin/keepup, ./]
    cache:
      method: hash # 'hash' (default) or 'mtime'
      reads: ["**/*.go", "go.mod", "go.sum"] # inputs; globs support **
      writes: ["bin/keepup"] # optional; must still exist for a hit
```

| Field    | Default      | Meaning                                                                                                         |
| -------- | ------------ | --------------------------------------------------------------------------------------------------------------- |
| `method` | `hash`       | `hash` reads file contents (correct); `mtime` uses modtime+size (faster, coarser).                              |
| `reads`  | — (required) | Input paths/globs. The fingerprint also folds in `command` + `params`, so changing the command busts the cache. |
| `writes` | `[]`         | Output paths/globs. If any declared output is missing, the cache is treated as a miss and the group re-runs.    |

Mechanics:

- The fingerprint is stored under `settings.cache-dir` (default
  `.keepup-cache`), one JSON file per group. Point `cache-dir` at a shared
  volume to share hits across machines/CI.
- Globs use `**` (via doublestar), so `src/**/*.go` works.
- `keepup run --no-cache` ignores existing entries and forces every group to
  run (entries are still refreshed afterwards).
- Caching is per-group opt-in: groups without a `cache:` block always run.

---

## `flows`

A `flow` is a named pipeline that composes groups. Each flow picks a
scheduling `mode`.

```yaml
flows:
  <flow-name>:
    description: "..." # optional, shown by `keepup list`
    mode: step # "step" (default) or "dag"
    steps: [...] # step mode only
    run: [...] # dag mode only
```

You can declare multiple flows in one file — the same `groups` are typically
reused across them.

### Step mode (default)

Steps run in declaration order. Within a step, the listed groups run in
parallel; a barrier separates consecutive steps. Outputs from earlier steps
are visible to later ones.

```yaml
flows:
  ci:
    description: "Linear CI flow"
    mode: step
    steps:
      - run: [vet, build] # vet and build run concurrently
      - run: [test] # test runs after both of step 1 finish
      - run: [package]
```

Step mode is the right default: order is visible at a glance and barriers
make behaviour predictable.

### DAG mode

DAG mode drops explicit steps. Groups schedule topologically from the data
graph formed by `{{ output.X }}` references — a group starts the moment all
groups it references have completed. Independent branches run in parallel
automatically.

```yaml
flows:
  ci-dag:
    description: "Same shape, scheduled topologically"
    mode: dag
    run: [vet, build, test, package]
```

For DAG mode the engine validates that the data graph is **acyclic** before
running anything.

When should you use each?

| Use case                                                               | Mode                                    |
| ---------------------------------------------------------------------- | --------------------------------------- |
| The order is part of your mental model; explicit barriers are valuable | step                                    |
| You want maximum throughput and the data deps speak for themselves     | dag                                     |
| You don't yet know the order — let references define it                | dag                                     |
| You want to share groups across pipelines with different shapes        | either; multiple flows in the same file |

Both modes share the same `Runner`, output capture, env merging,
`max-concurrency`, and `--dry-run` semantics; they only differ at
scheduling time.

### Timeout and retries

A flow can declare a control envelope that wraps each group's command run:

```yaml
flows:
  release:
    timeout: 10m # default per-group timeout for the whole flow
    retries: 1 # default retries on failure
    steps:
      - run: [build]
        timeout: 5m # override for this wave
        retries: 2
      - run: [smoke]
        timeout: 30s
```

| Field     | Where            | Meaning                                                                                                                 |
| --------- | ---------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `timeout` | flow and/or step | A Go duration (`30s`, `5m`, `1h`). Each command **attempt** is cancelled if it exceeds this. Empty/absent = no timeout. |
| `retries` | flow and/or step | Number of **additional** attempts after the first failure. `0` = no retry.                                              |

Resolution: a step's non-empty `timeout` / non-zero `retries` override the
flow defaults; otherwise the flow values apply. In **dag mode** there are no
steps, so the flow-level values are the only envelope.

Semantics:

- The envelope wraps only the **command run** — gating predicates (`require`,
  `skip-if`) and cache lookups are not retried or timed out.
- Each retry attempt gets its own fresh timeout.
- Between attempts there is a short backoff (`base × attempt`); it respects
  Ctrl-C / context cancellation.
- A cache write happens only after a successful attempt, so a timed-out or
  failed run never poisons the cache.

---

## `default`

```yaml
default: ci
```

The flow to run when `keepup run` is invoked without an argument. Must point
at an existing flow; otherwise the file is rejected at parse time.

---

## CLI subcommands

```sh
keepup run [flow]            # run the named flow, or the default
keepup watch [flow]          # re-run a flow when its cache.reads inputs change
keepup list                  # show declared flows + descriptions
keepup list groups           # show declared groups
keepup validate              # parse + validate; no execution
keepup graph [flow]          # emit a Mermaid diagram of the data DAG
keepup migrate <path>        # convert a legacy v1 file to v2
keepup version
```

`keepup watch` watches the union of `cache.reads` globs across the chosen
flow's groups, re-running the flow on any change. Because caching short-circuits
unchanged groups, only the work that actually depends on a changed file
re-executes. The flow must have at least one group with a `cache.reads` block,
otherwise there is nothing to watch.

Global flags:

| Flag                  | Purpose                                                                    |
| --------------------- | -------------------------------------------------------------------------- |
| `-c, --config <path>` | Override the config-file path (defaults to `~/.config/keepup/keepup.yml`). |
| `-d, --dry-run`       | Skip the runner; log what would run. Stacks on top of `settings.dry-run`.  |
| `-v, --verbose`       | Dump the parsed config before running.                                     |

`run`-only flags:

| Flag         | Purpose                                                            |
| ------------ | ------------------------------------------------------------------ |
| `--no-cache` | Ignore cached results and run every group (entries still refresh). |

`keepup graph <flow>` prints a Mermaid `graph TD` showing the data DAG that
emerges from the `{{ output.X }}` references — useful as a sanity check
regardless of whether you use step or dag mode.

---

## Worked example

A full file that exposes three flows over the same set of groups:

```yaml
version: 2

settings:
  logging: { level: info, pretty: true }
  max-concurrency: 4

env:
  GOFLAGS: "-trimpath"

groups:
  - { name: vet, command: go, params: [vet, ./...] }
  - { name: build, command: go, params: [build, -o, bin/keepup, ./] }
  - { name: test, command: go, params: [test, -race, ./...] }
  - {
      name: package,
      command: tar,
      params: [czf, dist/keepup.tar.gz, bin/keepup],
    }

default: ci

flows:
  ci:
    description: "Vet+build in parallel, then test, then package"
    mode: step
    steps:
      - run: [vet, build]
      - run: [test]
      - run: [package]

  ci-dag:
    description: "Same pipeline, scheduled by data deps"
    mode: dag
    run: [vet, build, test, package]

  quick:
    description: "Just build, for inner-loop iteration"
    mode: step
    steps:
      - run: [build]
```

Run with:

```sh
keepup run            # uses default → ci
keepup run quick      # fast iteration
keepup run ci-dag     # try the DAG scheduler
keepup graph ci-dag   # visualize what dag mode will do
```

---

## See also

- [FAQ](FAQ.md) — including v1 → v2 migration walkthrough
- [Usage](USAGE.md) — short tour of common commands
