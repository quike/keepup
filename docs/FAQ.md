# FAQ

Frequently-asked questions about keepup. For the canonical schema, see
[CONFIG](CONFIG.md).

---

## Migration: v1 → v2

### Is my v1 file still supported?

**No.** v2 is a deliberate clean break. Loading a `version: 1` file fails
with:

```sh
load configuration: unsupported schema version 1: this binary only supports version 2
```

The error is explicit on purpose: you should *see* that you're on the wrong
schema, not have keepup silently transform your file.

### Why a clean break instead of a compatibility shim?

Three reasons:

1. **One mental model.** v1's `execution:` block hard-coded an ordering of
   "groups" with no separation between unit and composition. v2 splits those
   concerns: `groups` are atomic and reusable; `flows` compose them.
   Maintaining both formats would have meant two engines, two reference-
   resolution paths, and two validation rules — keeping the second one
   right indefinitely is more work than a one-shot rewrite of your file.
2. **The file is the source of truth.** keepup never silently rewrites your
   YAML on disk, so a compat shim would have to live forever. A clean break
   means the file you read is the file the engine ran.
3. **It's an early project.** There is no installed base big enough to
   justify a permanent shim. Today is the cheapest moment.

If you do want an automated converter, a dedicated `keepup migrate <path>`
subcommand is the cleanest follow-up (file an issue if you want it
prioritized). The engine itself would stay single-schema.

### What changed in the YAML?

| v1 | v2 |
|---|---|
| `version: 1` | `version: 2` |
| `execution:` (single, top-level) | `flows: {<name>: {...}}` (one or many) |
| `execution[i].group: [a, b]` | inside a `step`-mode flow: `steps[i].run: [a, b]` |
| (no equivalent — only one pipeline per file) | `default: <flow>` selects the implicit target |
| (no equivalent — order was always explicit) | `mode: dag` opts into topological scheduling |
| (no equivalent) | `keepup list`, `keepup validate`, `keepup graph` |

`groups` themselves are unchanged — `name`, `command`, `params`, `env`,
`shell`, `description` all behave the same way.

### How do I migrate by hand?

Two-step recipe:

1. Bump `version: 1` to `version: 2`.
2. Rename `execution:` to a flow under `flows:`, and rename each
   `group: [...]` to `run: [...]` inside `steps:`. Add a `mode: step` (or
   leave it out — `step` is the default).

A real example lives in this repo:

- v1 source: [`internal/config/test-resources/config-valid-example-v1.yml`](../internal/config/test-resources/config-valid-example-v1.yml)
- v2 migration: [`internal/config/test-resources/config-valid-example-v2.yml`](../internal/config/test-resources/config-valid-example-v2.yml)

The same content side-by-side, plus the v2 file exposes two additional
partial flows (`brew`, `fish`) that v1 couldn't express in one document.

### What about `{{ output.X }}` references?

Same syntax. The references are validated more strictly now:

- v1 silently left `{{ output.X }}` un-substituted if `X` was missing.
- v2 rejects the file at load time with a precise error like
  `flow "ci" step 2: group "consumer" references {{ output.producer }}, but "producer" is not produced by an earlier step`.

That class of bug used to be a runtime surprise — now it's a parse-time
failure.

---

## Flows and modes

### When do I use step mode vs. dag mode?

| Use case | Pick |
|----------|------|
| You want explicit, line-readable order with barriers between phases | **step** |
| You want maximum throughput; the data deps already describe the order | **dag** |
| You're not sure yet — let `{{ output.X }}` references describe the shape | **dag** |
| You want the same `build` group reused in three different pipeline shapes | **either**; just declare three flows |

Both modes honor `max-concurrency`, `--dry-run`, context cancellation, and
share the same `Runner` / `OutputStore` / `Expander` / `Logger`. Switching
modes for a flow is a one-line change.

### Can a flow reference another flow?

Not directly. The composition unit is the **group**, not the flow. If two
flows share work, declare the shared groups once and list them in each
flow. This keeps the model simple — flows are leaves, groups are the
shared building blocks.

### Why are `groups` defined separately from `flows`?

So that a group is a pipeline-agnostic unit. The same `build` group can
appear in `ci`, `release`, and `quick` flows without duplication. In
contrast, frameworks that put dependencies on the unit (`task.deps: [...]`)
end up forcing one universal shape per task.

### How do I run just a single group?

Declare a tiny flow:

```yaml
flows:
  quick:
    mode: step
    steps:
      - run: [build]
```

Then `keepup run quick`. (The pre-v2 `--group <name>` shortcut is gone —
flows now play that role explicitly.)

---

## Output and references

### What does `{{ output.X }}` actually capture?

`X`'s combined stdout + stderr, with surrounding whitespace trimmed when
substituted into a template. The raw value is kept verbatim in the output
store.

### When are outputs visible to a group?

- **Step mode**: when group `G` runs in step N, it sees the outputs of every
  group from steps `1..N-1`. Same-step siblings are *not* visible — that
  would be a race. References that target same-step or later-step groups
  are rejected at parse time.
- **DAG mode**: when group `G` runs, it sees the outputs of every group in
  its transitive predecessor set (i.e., every group it depends on, directly
  or indirectly through references). The snapshot is taken at the moment
  `G` becomes ready.

### Is the captured output deterministic?

The string keepup substitutes is whatever the child process wrote to stdout
and stderr, in the order they were written. If your downstream depends on a
specific format, make the upstream produce a stable format (one line, JSON,
etc.).

---

## Execution

### Why are my params not being shell-expanded?

By default keepup runs `command` with `params` as a real argv list — no
shell parses the line. That's a deliberate safety choice (no command
injection from interpolated outputs, no surprises from spaces).

If you actually want shell features (`*` globs, `$()`, pipes, `&&`), opt
into shell mode by setting `shell:` to the path of a shell:

```yaml
- name: tail-logs
  command: "tail -f /var/log/*.log | grep ERROR"
  shell: /bin/sh
```

### What controls parallelism?

`settings.max-concurrency`. `0` (the default) means unbounded — both step-
mode parallel waves and dag-mode ready-queues will fan out as widely as the
graph allows. Any positive value caps the number of groups running
concurrently.

### How is Ctrl-C handled?

The signal is delivered through `context.Context` from the CLI down through
`Engine.RunFlow` and into `Runner.Run`, which uses `exec.CommandContext`.
The child process is killed and the flow returns the context error. All
schedulers honor it.

### What happens to in-flight siblings when a group fails?

The first error in a step (step mode) or anywhere in the DAG (dag mode)
cancels its siblings via the shared `errgroup` context. The flow then
returns the wrapped error. Subsequent steps are not started.

---

## CLI

### What replaces the old `--group <name>` flag?

Declare a flow that runs just that group:

```yaml
flows:
  quick:
    steps:
      - run: [<group-name>]
```

Then `keepup run quick`. This is more discoverable (`keepup list` shows it),
composable (you can build it up over time), and consistent with how every
other invocation works.

### How do I see what flows and groups are available?

```sh
keepup list           # flows + descriptions; default flow is starred
keepup list groups    # groups + descriptions
keepup validate       # parse and report; useful before running anything
```

### Can I visualise the execution graph?

Yes:

```sh
keepup graph              # default flow
keepup graph <flow>       # a specific flow
```

The output is a Mermaid `graph TD` block — pipe it into any Markdown
preview, GitHub renders it natively. The graph shows the data DAG (the
references), which is identical for step-mode and dag-mode versions of the
same flow.

---

## Project / philosophy

### Is keepup a taskfile.dev clone?

No, and it tries to stay deliberately distinct. Vocabulary (`groups` /
`flows` instead of `tasks` / `deps`), composition model (groups are
pipeline-agnostic, flows compose them — no `deps:` baked into a task), and
the dual scheduling modes are the main differentiators. taskfile is a great
project; this one has different opinions about where the boundary between
"unit" and "composition" should sit.

### Can I add my own runner / output store / logger?

The engine is built around small interfaces (`Runner`, `OutputStore`,
`Expander`, `Logger`) and uses functional options (`WithRunner`,
`WithOutputStore`, …). At the moment those extension points live inside
`internal/`, but they're designed to be cheap to expose as a public API
when there's demand.
