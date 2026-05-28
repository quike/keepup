// Package result defines RunResult, the structured outcome of running a group.
// It lives in its own leaf package so engine, cache, and template can all
// import it without creating an import cycle (template cannot import engine).
package result

// RunResult is the structured outcome of evaluating a group. Templates access
// it via `(out "x").Field`; the engine stores it in OutputStore; the cache
// persists it across runs.
//
// The zero value is meaningful: Status == "" appears only for groups that
// were never declared or never reached the store. Every declared, evaluated
// group ends up with one of the four known statuses ("ok", "skipped",
// "cached", "dry-run").
type RunResult struct {
	// Stdout is the captured stdout only (independent of Stderr).
	Stdout string `json:"stdout,omitempty"`
	// Stderr is the captured stderr only.
	Stderr string `json:"stderr,omitempty"`
	// Output is the chronologically interleaved merge of stdout and stderr,
	// matching the historical (pre-structured-outputs) capture behavior.
	// The `output "x"` template function returns strings.TrimSpace(Output).
	Output string `json:"output,omitempty"`
	// ExitCode is the process exit code. Always 0 in stored results today
	// (non-zero aborts the flow before storage). Laid down for a future
	// soft-fail / continue-on-error feature.
	ExitCode int `json:"exitCode,omitempty"`
	// DurationMs is wall-clock milliseconds for the command run. 0 for
	// skipped and cache-hit groups.
	DurationMs int64 `json:"durationMs,omitempty"`
	// Status is one of "ok", "skipped", "cached", "dry-run". An empty Status
	// indicates a never-stored group.
	Status string `json:"status,omitempty"`
}
