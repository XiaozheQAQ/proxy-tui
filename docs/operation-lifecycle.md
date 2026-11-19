# Mutation operation lifecycle

Mutation requests may carry an `operation_id`. The supervisor records:

    PENDING -> RUNNING -> SUCCEEDED | FAILED

- A duplicate ID while RUNNING returns `OPERATION_IN_PROGRESS`.
- A duplicate ID after completion returns the cached response.
- `proxy operation status <id>` returns UNKNOWN, RUNNING, SUCCEEDED, FAILED or EXPIRED
  without executing the operation.
- Results are kept for five minutes, bounded to 256 entries.
- Retries after a lost response reuse the same operation ID, so a repeated start cannot
  launch a second core.
- The cache stores RPC payloads only; subscription tokens and API secrets are not logged.
- A supervisor restart drops the cache; in-flight operation results then become UNKNOWN.
