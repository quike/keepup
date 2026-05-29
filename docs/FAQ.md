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

The error is explicit on purpose: you should _see_ that you're on the wrong
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

### Is there an automated converter?

Yes — `keepup migrate`:

```sh
keepup migrate ~/.config/keepup/keepup.yml            # print v2 to stdout
keepup migrate old.yml -o keepup.yml                   # write to a file
keepup migrate old.yml --flow ci                       # name the generated flow
```

It preserves groups, env, and settings, turns the single v1 `execution:` block
into one step-mode flow (the parallel waves are kept), and validates the result
against the v2 parser before emitting — so a successful migration is guaranteed
to load. The engine itself stays single-schema; `migrate` is the only code that
understands v1.

### What changed in the YAML?

| v1                                           | v2                                                |
| -------------------------------------------- | ------------------------------------------------- |
| `version: 1`                                 | `version: 2`                                      |
| `execution:` (single, top-level)             | `flows: {<name>: {...}}` (one or many)            |
| `execution[i].group: [a, b]`                 | inside a `step`-mode flow: `steps[i].run: [a, b]` |
| (no equivalent — only one pipeline per file) | `default: <flow>` selects the implicit target     |
| (no equivalent — order was always explicit)  | `mode: dag` opts into topological scheduling      |
| (no equivalent)                              | `keepup list`, `keepup validate`, `keepup graph`  |

`groups` themselves are unchanged — `name`, `command`, `params`, `env`,
`shell`, `description` all behave the same way.

### How do I migrate by hand?

Prefer `keepup migrate` (above); but if you want to do it manually it's a
two-step recipe:

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

| Use case                                                                  | Pick                                 |
| ------------------------------------------------------------------------- | ------------------------------------ |
| You want explicit, line-readable order with barriers between phases       | **step**                             |
| You want maximum throughput; the data deps already describe the order     | **dag**                              |
| You're not sure yet — let `{{ output.X }}` references describe the shape  | **dag**                              |
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
  group from steps `1..N-1`. Same-step siblings are _not_ visible — that
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

### Can I use functions/pipes in params, not just `{{ output.X }}`?

Yes. `command` and `params` are Go templates with the sprig library plus
`output "name"` and `env "KEY"` helpers, so `{{ output "sha" | trunc 7 }}`,
`{{ env "CI" | default "local" }}`, conditionals, and the rest of sprig all
work. The legacy `{{ output.X }}` form is still accepted (rewritten to
`{{ output "X" }}` under the hood), so existing configs are unaffected.

### My param has a literal `{{` and now errors — why?

Params are templates now, so a bare `{{` starts a template action. Escape a
literal with `{{ "{{" }}`. This only comes up if you genuinely need brace
characters in an argument; normal shell/command text is unaffected.

### Why did my `{{ output.X }}` stop trimming / start trimming?

It still trims. `output` returns the referenced group's stdout with
surrounding whitespace removed, exactly like the original expander — so a
producer printing `banana\n` substitutes as `banana`.

### When do I use `output` vs `out`?

`output "x"` returns the trimmed merged stdout+stderr as a string — use it
when you want to pipe through sprig string functions or embed in a
command/param. `out "x"` returns a structured value: use it when you want to
read a specific field (`.ExitCode`, `.DurationMs`, `.Stderr`, `.Status`). Both
are fully supported; `output` is unchanged from earlier versions.

### Why does `(out "x").ExitCode` have a capital `E`?

`out` returns a Go struct, and Go templates access struct fields case-sensitively
using the Go field name. Use `.Stdout`, `.Stderr`, `.Output`, `.ExitCode`,
`.DurationMs`, `.Status` (all capital first letter).

### Will my existing templates break?

No. `output "x"` keeps its exact contract — same trimmed string, same sprig
pipe support. Two changes worth flagging:

- The cache fingerprint format bumped once, so the first run after upgrade
  rebuilds every cached step. Steady-state cache hits return on the next run.
- A `skip-if:`-skipped group used to expose its prior cached output through
  `{{ output "x" }}`; it now exposes empty string. Templates that read a
  skipped producer's output now see `""`. Use `(out "x").Status == "skipped"`
  to branch on skip explicitly.

### Does `--dry-run` interact with structured outputs?

Yes — in dry-run, every group that would run is instead stored with
`Status: "dry-run"` (no actual command executes, but `when:` predicates still
evaluate against env/outputs to surface the real control flow). A dry-run
group's `Output` is empty.

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

### How do timeouts and retries work?

Declare them on a flow (the default for every group) and/or on a step (an
override for that wave). `timeout` is a Go duration applied to each command
**attempt**; `retries` is the number of additional attempts after the first
failure. They wrap only the command run — `require`/`skip-if` predicates and
cache lookups are never retried or timed out. Each attempt gets a fresh
timeout, there's a short backoff between attempts (which respects Ctrl-C), and
the cache is written only on success, so a failed/timed-out run can't poison
it. In dag mode there are no steps, so the flow-level values are the only
envelope.

### Can I make a single group in a dag flow conditional?

Yes. Instead of a bare group name, write a map with `group:` and `when:` keys:

```yaml
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

`when:` uses the same template engine and falsey set as step-mode `when:`
(`""`, `"false"`, `"0"`, `"no"`, `"off"`). The map form accepts **only**
`group` and `when` — `command`, `params`, and other group fields belong on the
group definition in `groups:`. An unexpected key is a config error.

References inside `when:` (e.g. `output "test"`) become scheduling edges:
`test` is guaranteed to finish before the predicate runs, and the reference
participates in the cycle check.

Bare-string entries in `run:` are unchanged — the map form is purely additive.

### What happens to groups that depend on a skipped dag group?

They are **cascade-skipped**: a group that depends (directly or transitively)
on a skipped group's output cannot run, because the producer's output is
absent. Each cascade-skipped group emits a `group.end` event with
`"status":"skipped"` and a `"reason"` explaining which upstream group was
skipped.

Example: a `release` flow where `deploy` is gated on `DEPLOY=true` and
`notify` depends on `deploy`:

```sh
$ keepup run release --config keepup.yml --events -
{"event":"flow.start","flow":"release","mode":"dag"}
{"event":"group.end","group":"deploy","status":"skipped","reason":"when"}
{"event":"group.end","group":"notify","status":"skipped","reason":"upstream \"deploy\" skipped"}
{"event":"group.end","group":"build","status":"ok","durationMs":2}
{"event":"flow.end","flow":"release","status":"ok","durationMs":2}
```

`"reason":"when"` marks the directly-gated group; `"reason":"upstream
\"deploy\" skipped"` marks groups skipped because their producer was skipped.
A cascade skip is not a failure — the flow ends with `"status":"ok"` as long
as no group actually errored.

### Does `--dry-run` evaluate `when:` predicates?

Yes. `when:` predicates have no side effects, so they evaluate normally in
`--dry-run`, and the dry run reveals the real control flow — including which
groups would be skipped and which dependents would cascade. A `when:`-skipped
group emits `"status":"skipped"` (skip wins over dry-run because the decision
happens before the group would be launched); other groups emit
`"status":"dry-run"`.

### How does `when:` differ from a group's `skip-if`?

`when:` is on a **step** (step mode) or a **dag run entry** (dag mode) and
decides whether that wave or individual group runs; `skip-if:` is on a
**group** and decides whether that one group runs. `when:` is a template
predicate (uses `output`/`env`/sprig, falsey = skip) evaluated against
prior outputs; `skip-if:` is a shell command (exit 0 = skip) run just before
the group. Use `when:` for "should this phase happen at all?" and `skip-if:`
for "is this group's work already done?".

---

## Caching and gating

### How does caching decide to skip a group?

A group with a `cache:` block is fingerprinted before it runs. The
fingerprint folds in the `method`, the `command`, the `params`, and the
contents (or mtime+size) of every file matched by `reads`. If the fingerprint
matches the stored one **and** every `writes` path still exists, keepup skips
the runner and replays the previously-captured output. Otherwise it runs and
refreshes the entry.

### Where is the cache stored? Is it safe to commit or share?

Under `settings.cache-dir` (default `.keepup-cache`), one JSON file per group
containing the fingerprint and the captured output. It's a build artifact —
add it to `.gitignore`. You _can_ point `cache-dir` at a shared volume to
share hits across machines or CI runners, since the fingerprint is
content-based.

### `hash` vs `mtime` — which method?

`hash` reads file contents, so it's correct even when a file is touched but
unchanged; `mtime` only stats files, so it's faster on large trees but will
re-run if a tool rewrites timestamps without changing content. Default is
`hash`; switch to `mtime` only if input reads become a measurable cost.

### How do I force a rebuild?

`keepup run --no-cache` ignores existing entries and runs everything (entries
are still refreshed afterward). Or delete the `cache-dir`.

### What's the difference between `skip-if` and `cache`?

`skip-if` is a **predicate you write** ("is this already done?"), evaluated by
running a shell snippet; exit 0 means skip. `cache` is **automatic
fingerprinting** of declared inputs. Use `skip-if` for cheap idempotency
checks (`test -d /some/dir`); use `cache` when "did the inputs change?" is the
real question. They compose — `require` → `skip-if` → `cache` → run.

### What does a skipped group publish for `{{ output.X }}`?

A `cache` hit replays the stored output. A `skip-if` skip publishes the last
cached output if the group also has a `cache:` block, otherwise an empty
string. Downstream references always resolve to _something_, never an error.

### Do predicates and caching run during `--dry-run`?

It depends on the predicate type. `when:` template predicates **do** evaluate
— they have no side effects and dry-run uses the results to reveal real control
flow (which groups would skip, which would cascade). `require` and `skip-if`
shell predicates do **not** run — no shells are spawned and the cache is not
consulted, so dry-run never touches disk.

---

## Watch mode

### What does `keepup watch` watch?

The union of the `cache.reads` globs across the chosen flow's groups. Those
declarations already describe each group's inputs, so watch reuses them as the
file set — no separate watch configuration. The flow must have at least one
group with `cache.reads`, otherwise there's nothing to watch and the command
errors.

### Why does watch reuse `cache.reads` instead of a dedicated `watch:` field?

Because the inputs that should trigger a rebuild are exactly the inputs that
define a cache hit. Reusing one declaration keeps the two features consistent:
the same change that busts the cache is the same change that triggers a re-run,
and unchanged groups are skipped on each pass.

### How does it avoid rebuilding everything on every keystroke?

Two layers. First, a short debounce (200ms) collapses bursts of editor events
into one re-run. Second, caching means the re-run only executes groups whose
inputs actually changed — the rest are cache hits.

### Does watch run once on start?

Yes. It performs an initial run immediately, then watches. A failing run is
logged but does not stop watching — fix the code and save again.

### How do I stop it?

Ctrl-C (SIGINT) or SIGTERM. The signal propagates through the context, the
in-flight run is cancelled, and `watch` returns cleanly.

### Are newly-created files/directories picked up?

New directories under a watched tree are added automatically as they appear, so
files created in them are seen. Brand-new top-level trees that didn't exist when
watch started are the one gap — restart watch if you add a whole new source root.

### Does `keepup watch` emit the same event stream as `keepup run`?

Yes. `keepup watch --events <path|->` emits the same `flow.start` / `group.start` / `group.end` / `flow.end` events as `run`, one envelope per re-run, plus a `watch.trigger` event before each re-run carrying the deduplicated, sorted list of file paths that triggered the debounced batch:

```
{"event":"watch.trigger","files":["src/main.go","src/util.go"]}
```

The initial run on startup emits no `watch.trigger` — the leading `flow.start` marks it. The `"watching N dir(s)…"` banner writes to stderr, so `--events -` yields pure JSON on stdout.

---

## CLI

### How do I get started quickly?

`keepup init` writes a small, valid starter `keepup.yml`:

```sh
keepup init            # → ./keepup.yml (project-local)
keepup init --global   # → ~/.config/keepup/keepup.yml (the default path)
keepup init --force    # overwrite an existing file
```

`./keepup.yml` is convenient for a repo you'll run with `keepup run --config keepup.yml`
(or from that directory). `--global` writes to the path keepup reads when no
`--config` is given, so afterward a bare `keepup run` just works. `--global`
and an explicit path are mutually exclusive. The scaffold defines two groups
and a step flow that demonstrates output piping, and it's guaranteed to parse.

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

### What is the JSON event stream and what can I use it for?

When keepup runs a flow it prints human logs. With `--events` it *also* emits a
second, separate stream: **one JSON object per line, one line per thing that
happens.** That's the whole idea.

```sh
keepup run ci --events run.jsonl
```

`run.jsonl` then contains:

```json
{"event":"flow.start","flow":"ci","mode":"step","time":"..."}
{"event":"group.start","group":"tests","time":"..."}
{"event":"group.end","group":"tests","status":"ok","durationMs":1240,"time":"..."}
{"event":"group.start","group":"deploy","time":"..."}
{"event":"group.end","group":"deploy","status":"skipped","durationMs":0,"time":"..."}
{"event":"flow.end","flow":"ci","status":"ok","durationMs":1310,"time":"..."}
```

Each line tells you **what happened** (`event`), **to which group**, **how it
ended** (`status`: `ok` / `failed` / `skipped` / `cache-hit` / `dry-run`), and
**how long it took** (`durationMs`).

**Why separate from logs?** Logs are for humans (prose, colors, wording that may
change). Events are for machines (fixed fields, stable shape). Parsing human
logs is fragile; the event stream is a contract you can build on.

**What it's good for:**

| Use | How |
|-----|-----|
| CI dashboards / timing | Read `durationMs` per group to find the bottleneck and track it over time. |
| Notifications | Watch for `flow.end` with `status:"failed"` and alert. |
| Cache effectiveness | Count `cache-hit` vs `ok` to see how much work was skipped. |
| Audit / history | Append the `.jsonl` files to keep a record of every run. |
| Custom tooling | Anything that needs to react to a run without scraping logs. |

**Example one-liners** (using `jq`, but any language parses JSON lines):

```sh
# Did the flow fail?
keepup run ci --events - | jq -e 'select(.event=="flow.end" and .status=="failed")' && echo ALERT

# Slowest group
keepup run ci --events - | jq 'select(.event=="group.end")' | jq -s 'max_by(.durationMs)'

# How many cache hits?
keepup run ci --events - | jq 'select(.status=="cache-hit")' | wc -l
```

It's independent of the human/JSON logging (`settings.logging`) — logs are for
people, events are for tooling. If you only run keepup by hand in a terminal you
don't need it; it shines when something *else* watches your runs.

Note: use `--events run.jsonl` (a file) or `--events -` (stdout). On stdout the
events mix with your commands' own output, so a file is usually cleaner for real
use.

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
