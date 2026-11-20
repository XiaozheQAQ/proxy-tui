# Mutation operation lifecycle

Mutation requests may carry an `operation_id`. The supervisor atomically claims the ID
before executing, fingerprints the request (operation name + SHA-256 of the payload), and
records:

    PENDING/RUNNING -> SUCCEEDED | FAILED

- A duplicate ID with the same fingerprint while RUNNING returns `OPERATION_IN_PROGRESS`.
- A duplicate ID with the same fingerprint after completion returns the cached response.
- A duplicate ID with a different fingerprint is rejected with `BAD_REQUEST`; no second
  side effect is executed.
- `proxy operation status <id>` returns UNKNOWN, RUNNING, SUCCEEDED, FAILED or EXPIRED
  without executing the operation.
- RUNNING entries never expire and are never evicted by the 256-entry bound. Terminal
  results are kept for five minutes measured from completion.
- Retries after a lost response reuse the same operation ID. The remote client polls
  operation.get instead of blindly re-sending the mutation.
- A supervisor restart drops the cache; in-flight results then become UNKNOWN. This is a
  process-lifetime cache, not a WAL. Preconditions in the operations (core status checks,
  duplicate-profile detection) still make the common retries safe, but cross-crash
  idempotency is not claimed.
- Read operations keep a 30s deadline; mutations use a 15m business deadline.
