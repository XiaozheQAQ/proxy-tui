# Supervisor lifecycle and safe Ensure

## Persisted state

- `supervisor-state.json`: live PID/endpoint/version while the supervisor runs.
- `supervisor-lifecycle.json`: `RUNNING` or `INTENTIONALLY_STOPPED`.

A graceful `proxy supervisor stop` writes `INTENTIONALLY_STOPPED`. A crash or external
kill leaves the last `RUNNING` record behind.

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

The existing core identity checks are retained. This round did **not** run a FakeCore
adoption test, so "crash, adopt the still-running core" is NOT TESTED. Recovery never kills
a core to make a supervisor start.
