# Real VyOS Lab Smoke

This directory contains manual/on-demand lab validation for the real VyOS path.
It is release evidence, not normal PR CI.

Lab artifacts are collected locally for review. They are not uploaded
automatically by the manual GitHub self-hosted workflow.

## What This Proves

The configure lab smoke proves:

- configure is submitted through real NATS and JetStream KV
- the running agent handles the command in real configure mode
- rendered config is applied to a real VyOS VM/device
- the local agent state checkpoint contains the submitted UUID
- resubmitting the same UUID reports the already-in-sync path
- evidence artifacts are collected for PR or release review

## Smoke vs Lab

`tests/smoke` contains CI-friendly smoke scripts. They start local NATS and use
placeholder behavior, so they do not need a VyOS device.

`tests/lab` contains manual real-device scripts. They require a reachable VyOS
VM/device, credentials, real NATS, and an agent configured for real mode.

`real-vyos-configure-smoke.sh` runs in manual dependency mode only: it assumes
the NATS server and the VyOS agent are already running and focuses on configure
submission, verification, and evidence collection.

## Required Lab Topology

- A real or disposable VyOS VM/device reachable over SSH.
- A NATS server reachable by both the controller script and the agent.
- A running `vyos-nats-agent` configured for the same target and NATS server.
- `agent.configure.mode: real` for real configure validation.
- A known-safe desired config fixture from `tests/lab/configs`.

Use a disposable lab target. The fixtures are intended to be small WAN/WAN+LAN
smoke configs, but they still change VyOS configuration.

## Manual Prerequisites

Start NATS manually on the Ubuntu host, for example:

```bash
nats-server -js -p 4222
```

Install and start the VyOS agent manually inside the VyOS VM, for example:

```bash
sudo install -m 0755 ~/vyos-nats-agent /usr/local/bin/vyos-nats-agent
nohup /usr/local/bin/vyos-nats-agent --config ~/vyos-nats-agent.yaml >/tmp/vyos-nats-agent.log 2>&1 &
```

## Required Environment

```bash
export REAL_VYOS_LAB_ACK=I_UNDERSTAND
export NATS_URL=nats://192.168.76.69:4222
export VYOS_TARGET=vyos
export VYOS_HOST=192.168.76.2
export VYOS_USER=vyos
export STATE_PATH=/tmp/vyos-nats-agent/state.json
export REMOTE_AGENT_LOG=/tmp/vyos-nats-agent.log
export DESIRED_CONFIG_FILE=tests/lab/configs/desired-vyos-wan-only-config.json
export ARTIFACT_DIR=tests/lab/artifacts/manual-run-001
export VYOS_SHOW_CONFIG_COMMAND="/opt/vyatta/bin/vyatta-op-cmd-wrapper show configuration commands"
```

Use exactly one SSH auth method:

```bash
export VYOS_PASSWORD=vyos
```

or:

```bash
export VYOS_SSH_KEY=/path/to/private/key
```

Optional:

```bash
export CONFIG_UUID=cfg-lab-$(date +%s)
export RPC_ID=real-vyos-configure-$(date +%s)
export RESUBMIT_SAME_UUID=true
export EXPECTED_VYOS_MATCH=OLG_APPLY_SMOKE_TEST
export KEEP_WORK_DIR=false
```

## Run Configure Smoke

```bash
./tests/lab/real-vyos-configure-smoke.sh
```

Example full run:

```bash
export REAL_VYOS_LAB_ACK=I_UNDERSTAND
export NATS_URL=nats://192.168.76.69:4222
export VYOS_TARGET=vyos
export VYOS_HOST=192.168.76.2
export VYOS_USER=vyos
export VYOS_PASSWORD=vyos
export STATE_PATH=/tmp/vyos-nats-agent/state.json
export REMOTE_AGENT_LOG=/tmp/vyos-nats-agent.log
export DESIRED_CONFIG_FILE=tests/lab/configs/desired-vyos-wan-only-config.json
export ARTIFACT_DIR=tests/lab/artifacts/manual-run-001
export VYOS_SHOW_CONFIG_COMMAND="/opt/vyatta/bin/vyatta-op-cmd-wrapper show configuration commands"
./tests/lab/real-vyos-configure-smoke.sh
```

The script writes evidence into `ARTIFACT_DIR`, including:

- `phase9-summary.md`
- `configure-status.jsonl`
- `configure-result.jsonl`
- `agent.log`
- `vyos-before.txt`
- `vyos-after.txt`
- `state.json`
- `commands-run.txt`
- `environment-summary.txt`

The script does not print passwords, tokens, or private keys. It copies the
already-running agent's remote log into `agent.log` after all configure checks
complete. Failed runs also attempt best-effort remote agent log collection
before exit so the artifact set still includes agent-side evidence when
possible.

## Action Trace Smoke

The action trace smoke test submits a trace request through NATS. It runs an inline controller that starts a local HTTP upload server, receives the uploaded PCAP file from the agent, verifies the PCAP magic number header, and records status and result envelopes.

To run the trace smoke test:

```bash
./tests/lab/real-vyos-action-trace-smoke.sh
```

The script writes evidence into `ARTIFACT_DIR`, including:

- `phase9-summary.md`
- `action-status.jsonl`
- `action-result.jsonl`
- `captured.pcap`
- `agent.log`
- `commands-run.txt`
- `environment-summary.txt`


## Collect Evidence

After a local lab run:

```bash
ARTIFACT_DIR=tests/lab/artifacts/manual-run ./tests/lab/collect-lab-evidence.sh
```

After a GitHub self-hosted lab workflow run, evidence is also collected locally
on the runner under a per-run directory in the checked-out workspace, for
example `tests/lab/artifacts/github-actions/run-<run_id>-attempt-<run_attempt>`.
The workflow recreates that directory at job start and prints the exact path at
the end of the run.

Artifacts are not uploaded automatically by the GitHub workflow. Before
attaching the artifact directory, or an archive of it, to a PR or release note,
review and sanitize the files and remove any lab-specific data that should not
leave the lab.

## Secret Safety

- Do not enable shell tracing with `set -x`.
- Do not put passwords, tokens, or private keys in `commands-run.txt`.
- `environment-summary.txt` records whether secret variables are set, not their
  values.
- Review `agent.log` and VyOS output before sharing outside the lab.
- Review `vyos-before.txt`, `vyos-after.txt`, `state.json`,
  `commands-run.txt`, and `environment-summary.txt` before attaching artifacts
  to a PR or release note.

## Known Limitations

- The configure script validates a configurable marker string in VyOS output;
  set `EXPECTED_VYOS_MATCH` if your fixture uses a different marker.
- Different lab topologies may require a different desired config fixture.
- `/tmp` state paths are acceptable for disposable lab runs but should be
  configured appropriately for production deployments.

## Rollback / Revert Notes

Before running the configure smoke, the script captures `vyos-before.txt`.
After the run, it captures `vyos-after.txt`.

To revert, use the lab's standard VyOS rollback process or submit a known-good
desired config through the same NATS/KV path. Do not run the lab smoke against a
production router.
