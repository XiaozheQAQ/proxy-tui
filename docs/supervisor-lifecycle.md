# Supervisor lifecycle and safe Ensure

## Persisted state

- `supervisor-state.json`: live PID/endpoint/version while the supervisor runs.
- `supervisor-lifecycle.json`: `RUNNING` or `INTENTIONALLY_STOPPED`.

A `proxy supervisor stop` request first persists `INTENTIONALLY_STOPPED` (with instance ID,
PID, start time, version and source `supervisor.stop`) before acknowledging or cancelling
the server. If that write fails, stop is refused with an error and the supervisor keeps
running. A crash or external kill therefore leaves the last `RUNNING` record behind.

The record carries `version`, `instance_id`, `status`, `pid`, `started_at`, `updated_at` and
`source`. It is written atomically.

## Client behavior

- Dial succeeds: use the supervisor.
- Dial fails and lifecycle is `INTENTIONALLY_STOPPED`: return
  `SUPERVISOR_INTENTIONALLY_STOPPED`; do not spawn a new supervisor.
- Dial fails and lifecycle is `RUNNING`/missing: treat as crashed/absent and Ensure.
- `proxy supervisor start` is explicit and clears the stop state by starting a supervisor.
- `proxy supervisor status` prints `INTENTIONALLY_STOPPED` without starting anything.

## Concurrency

Ensure is serialized across processes with `state/runtime/ensure.lock` plus the atomic
Named Pipe / socket bind. Five concurrent CLI clients after a forced supervisor kill
produced one supervisor process.

## Core adoption

Core adoption now verifies PID start time, executable path, binary SHA-256, config
SHA-256 and (best effort) the Clash API secret. Adapter-level tests cover verified adopt,
transient API degradation, legacy state refusal and safe Stop. A full Supervisor A/B crash
run with a real OS FakeCore is still **NOT TESTED**; recovery never kills a core to make a
supervisor start.
