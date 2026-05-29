# Usage

keepup is a YAML-driven task runner: declare reusable **groups** of
commands and compose them into one or more named **flows**. Each flow
runs either as ordered parallel waves (step mode) or topologically by
data deps (dag mode).

This page is a short tour. For the full schema, see [CONFIG](CONFIG.md).
For migration from v1, see [FAQ](FAQ.md).

## Config location

By default keepup reads `$HOME/.config/keepup/keepup.yml`. Override with
`-c` / `--config <path>`.

## A minimal v2 file

```yaml
version: 2

settings:
  logging: { level: info, pretty: true }
  max-concurrency: 2

groups:
  - name: brew-update
    description: "Refresh formula index"
    command: brew
    params: [update, -v]
  - name: brew-upgrade
    description: "Upgrade packages"
    command: brew
    params: [upgrade]
  - name: brew-cleanup
    description: "Remove old versions"
    command: brew
    params: [cleanup]

default: update

flows:
  update:
    description: "Daily brew maintenance"
    mode: step
    steps:
      - run: [brew-update]
      - run: [brew-upgrade]
      - run: [brew-cleanup]
```

## CLI cheatsheet

```sh
keepup init               # scaffold a starter keepup.yml (--global → ~/.config/keepup)
keepup run                # run the default flow
keepup run <flow>         # run a specific flow
keepup watch [flow]       # re-run a flow when its cache.reads inputs change
keepup list               # list flows (default starred)
keepup list groups        # list groups
keepup validate           # parse & reference-check; no execution
keepup graph [flow]       # emit a Mermaid diagram of the data DAG
keepup migrate <path>     # convert a legacy v1 file to v2
keepup version
```

Common flags:

- `-c, --config <path>` — point at a config file
- `-d, --dry-run` — log what would run; never invoke the runner
- `-v, --verbose` — dump the parsed config before running
- `--no-cache` (run only) — ignore cached results and run every group

## Watch mode

`keepup watch` turns the inner loop into a live one. It watches the files
declared in the `cache.reads` of the flow's groups and re-runs the flow on
each change; caching ensures only the affected groups actually execute.

```yaml
groups:
  - name: build
    command: go
    params: [build, ./...]
    cache:
      reads: ["**/*.go", "go.mod"]
flows:
  dev:
    steps:
      - run: [build]
```

```sh
keepup watch dev   # build now, then rebuild on every .go change; Ctrl-C to stop
```

The flow needs at least one group with a `cache.reads` block — that's the
watch set.

### Watching a flow with the event stream

`keepup watch` accepts the same `--events <path|->` flag as `keepup run`:

```bash
keepup watch ci --events -
```

The "watching N dir(s)…" banner writes to **stderr**, so stdout carries pure JSON. Each re-run emits a full `flow.start` / `group.*` / `flow.end` envelope, and every debounced batch of file changes emits a `watch.trigger` event listing the changed files:

```
{"event":"flow.start","flow":"ci","mode":"step"}
{"event":"group.end","group":"build","status":"ok","durationMs":1}
{"event":"flow.end","flow":"ci","status":"ok","durationMs":1}
{"event":"watch.trigger","flow":"ci","files":["x.txt"]}
{"event":"flow.start","flow":"ci","mode":"step"}
{"event":"group.end","group":"build","status":"ok","durationMs":4}
{"event":"flow.end","flow":"ci","status":"ok","durationMs":4}
```

The first envelope is the initial run on startup — it has no preceding `watch.trigger`. Pass `--events <file>` to write the stream to a file instead of stdout.

## Multiple flows over the same groups

The same groups can power many flows — that's the point of the split.
For example, a partial flow that only does the upgrade step:

```yaml
flows:
  upgrade-only:
    mode: step
    steps:
      - run: [brew-upgrade]
```

Then:

```sh
keepup run upgrade-only
```

## Output piping

A group's `params` can reference another group's captured stdout via
`{{ output.<name> }}`. Example:

```yaml
groups:
  - name: sha
    command: git
    params: [rev-parse, --short, HEAD]
  - name: tag
    command: echo
    params: ["release-{{ output.sha }}"]

flows:
  tag-it:
    mode: step
    steps:
      - run: [sha]
      - run: [tag]
```

In step mode the referenced group must appear in an earlier step. In dag
mode references determine the schedule order automatically.

## Shell mode (opt-in)

By default `command` + `params` is exec'd as a real argv — no shell
parsing, no injection risk. Set `shell:` to a shell path to opt into a
shell-interpreted single line:

```yaml
- name: pipe-something
  command: "ls -1 | wc -l"
  shell: /bin/sh
```

Use shell mode only when you actually need shell features (`|`, `$()`,
globs, …).

## Conditional dag groups

In dag mode a single `run:` entry can carry a `when:` predicate so it only
runs when a condition is met. The group is skipped (not failed) when the
predicate is falsey; any group that depends on its output is cascade-skipped.

```yaml
groups:
  - name: build
    command: go
    params: [build, -o, bin/app, ./]
  - name: test
    command: go
    params: [test, ./...]
  - name: deploy
    command: ./scripts/deploy.sh
    params: ['{{ output "test" }}']
  - name: report
    command: ./scripts/notify.sh

flows:
  ci:
    mode: dag
    run:
      - build
      - test
      - group: deploy
        when: '{{ eq (output "test") "pass" }}'
      - report
```

Run it and watch the event stream:

```sh
keepup run ci --config keepup.yml --events -
```

When tests pass, all groups run:

```
{"event":"flow.start","flow":"ci","mode":"dag"}
{"event":"group.end","group":"build","status":"ok","durationMs":3}
{"event":"group.end","group":"test","status":"ok","durationMs":4}
{"event":"group.end","group":"deploy","status":"ok","durationMs":3}
{"event":"group.end","group":"report","status":"ok","durationMs":3}
{"event":"flow.end","flow":"ci","status":"ok","durationMs":11}
```

When the predicate is falsey (e.g. `DEPLOY` unset in an env-gated flow),
`deploy` is skipped and any downstream group is cascade-skipped:

```
{"event":"flow.start","flow":"release","mode":"dag"}
{"event":"group.end","group":"deploy","status":"skipped","reason":"when"}
{"event":"group.end","group":"notify","status":"skipped","reason":"upstream \"deploy\" skipped"}
{"event":"group.end","group":"build","status":"ok","durationMs":2}
{"event":"flow.end","flow":"release","status":"ok","durationMs":2}
```

The flow still ends `"status":"ok"` — a skip is not an error.

## Gating on a previous group's exit status, duration, or skip

Structured outputs (`out "x"`) let you branch on any fact the engine knows
about a prior group:

```yaml
groups:
  - { name: probe, command: ./probe.sh }
  - { name: thorough-check, command: ./thorough.sh }

flows:
  diagnostic:
    mode: dag
    run:
      - probe
      - group: thorough-check
        when: '{{ gt (out "probe").DurationMs 500 }}'
```

`thorough-check` runs only when the probe took longer than half a second —
useful for adaptive flows that fall back to expensive checks only when a
cheap probe is inconclusive.

To branch on whether a sibling was skipped (rather than ran):

```yaml
when: '{{ eq (out "deploy").Status "skipped" }}'
```

When `out "x"` references a group not listed in the flow's `run:`, config
validation rejects it at load (identical rule to `output "x"`).

`output "x"` is unchanged — it still returns the trimmed merged stdout+stderr
and works in all sprig pipes. Use `out "x"` only when you need a specific
field like `.Status`, `.DurationMs`, or `.Stderr`.

## More

- [Configuration](CONFIG.md) — every field, defaults, semantics
- [FAQ](FAQ.md) — v1 → v2 migration, mode picking, output rules
