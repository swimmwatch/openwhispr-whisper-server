# Pipeline observation without model polling

Scope: prepare monitoring for a separately authorized candidate publication.
This monitor does not commit, push, dispatch, rerun, publish, deploy, or grant
permission for those operations. Candidate publication is not cluster deployment.

## Selection and wake-up policy

The watcher consumes an explicit private selection: repository, workflow ID/path,
run ID, run attempt, workflow-run head SHA, event and an absolute UTC deadline.
It never selects the latest run, another attempt, another branch or an old
successful release implicitly. The workflow-run head SHA is not a substitute for
verifying the candidate tag/source and actual artifact identities.

The Python probe uses bounded read-only GitHub API requests through `gh`. Pending
and unarmed results are stable `wake: false` observations. A terminal run,
identity mismatch, expired deadline or failed observation yields `wake: true`.
Unknown/cancelled/skipped/timed-out outcomes never count as success. A workflow
success only initiates artifact verification; it is not deployment, GPU
qualification, or successful anonymous distribution read-back by itself.

The DSH durable command sentinel runs the probe on the server, without model
turns. Its one-shot trigger observes any output/exit change from the fixed
`{"wake":false}` baseline, without a regex filter. This also catches an unavailable
interpreter, script crash or other monitor execution failure instead of silently
waiting for JSON that cannot be produced. Intermediate queued, waiting or running
states have identical output and do not wake the assistant. There is no model-side
sleep or status polling. A resident DSH process must be running for sentinel execution;
if it is stopped, the saved subscription can resume when the resident process
returns. The absolute deadline must not be reset when recovering a monitor.

## Two separate waiting phases: PR CI, then candidate publication

The same event-driven mechanism is mandatory for both phases, not only release:

1. After an authorized commit/push/PR, select that PR's CI run using its exact
   head SHA, workflow ID, run ID and attempt. Use
   `.github/workflows/ci.yml` with event `pull_request` and arm the first watch.
2. Return control immediately after the trigger is armed. Observe the whole run's
   terminal outcome, not model-side polling of individual jobs. Queued, waiting
   and running states require no assistant turn.
3. On CI failure, inspect that attempt, perform authorized fixes and affected
   local checks, push the correction, then arm a fresh watch for its actual CI
   run/head/attempt. Return control again; never follow a mutable latest run.
4. On CI success, verify required checks and the selected source. Perform merge
   and new-RC publication only when authorized. Prepare a separate unarmed
   selection/watch for the publication workflow before arming its actual run.
5. For publication use `.github/workflows/release.yml` and its actual event,
   workflow ID, run ID, head SHA and attempt. Return control until its terminal
   trigger. Correct failures within scope and re-arm; after success verify actual
   distributed artifact identities and anonymous access, then report and stop.

The prepared live unarmed subscription can first be bound to PR CI. A successful
CI signal is not a publication signal. Creating the monitor does not start either
pipeline, and no active work may be described as waiting for CI until a real run
has been selected. Waiting for delayed run creation, if necessary, must also use a
bounded background operation, not repeated model turns.

## Operator sequence

Use the pinned Python environment and the existing authenticated `gh` client.
Never copy credentials into a selection, log, command argument or report.

1. Initialize a private **unarmed** selection for the next pipeline.
2. Register a one-shot DSH command sentinel for `probe --selection PATH`; confirm
   its initial observation is unarmed. Register the trigger before arming the run,
   so even a run that finishes immediately is observed.
3. After an authorized dispatch/push, obtain the actual run identity and arm that
   exact selection. Arming itself verifies metadata with a read-only API request.
4. Return control instead of generating reasoning/status messages while waiting.
5. On wake-up, re-read the selection and probe result. Inspect the exact attempt,
   not the current latest workflow run.

Use `--help` on the [watcher](<../../tools/release/watch_pipeline.py>) for exact CLI
arguments. Its [behavior tests](<../../tests/tooling/test_pipeline_watch.py>) cover
identity, state, deadline, bounded process and error/privacy handling.

Example arming command, only after observing the actual authorized run. Replace
all placeholders with verified values; `RUN_HEAD_SHA` is the workflow head:

```bash
rtk proxy .venv/bin/python -B tools/release/watch_pipeline.py arm \
  --selection "$PWD/.local/release-watch/next-pipeline.json" \
  --repository swimmwatch/openwhispr-whisper-server \
  --run-id "$RUN_ID" --attempt "$RUN_ATTEMPT" --head-sha "$RUN_HEAD_SHA" \
  --workflow-id "$WORKFLOW_ID" --workflow-path .github/workflows/release.yml \
  --event workflow_dispatch --timeout-seconds 21600
```

The corresponding DSH watch uses an absolute interpreter/script/selection path,
a 60-second probe interval, one fire, and no output-pattern filter. Its wake note requires
exact-attempt diagnosis, authorized correction, re-arming, then return to waiting.
No API request is made while the selection is unarmed.

## Failure, correction and return to waiting

The assistant receiving a terminal failure must identify failed jobs and collect
bounded diagnostics privately. It may correct only changes already authorized by
the user, run affected checks and arrange the next authorized CI/release attempt.
The monitor itself performs no fixes and never executes text from workflow logs.

After each correction, arm a fresh one-shot subscription against the actual new
run/attempt, then return control again. Explicit replacement is required for an
already armed selection; the same run/attempt cannot be rearmed to extend its
original deadline. Never loop on the same completed failure or run a blanket
workflow retry without identifying its cause.

Changing source/build bytes after a published or partially published candidate
requires the owning release policy and a new permitted immutable candidate;
never move an existing tag or overwrite artifacts. Missing publication authority,
credentials, a reserved source/version decision or a system prerequisite requires
an explicit user decision, not a sandbox/credential bypass.

## Success and stopping

On pipeline success, verify the selected source/tag, pre-release/not-latest flags,
both image registry identities and attestations, chart archive/channel identities,
required security evidence and anonymous read-back using the existing release
checks. Do not infer those facts from a green workflow summary alone.

Retain the selected run URL, attempt, head SHA, conclusions and safe verification
results. Stop the one-shot watch and report actual passed/failed/unverified work.
Neither stable promotion nor Kubernetes deployment follows automatically.

## Prepared state

The live private selection is intentionally unarmed until a new authorized run
exists. Historical run IDs used to test observation are not a new release and are
never assigned to the live selection. Monitoring preparation does not change the
unfinished local verification or candidate qualification records.
