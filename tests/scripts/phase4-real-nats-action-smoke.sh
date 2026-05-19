#!/usr/bin/env bash
set -euo pipefail

# Phase 4 real-NATS action smoke test for vyos-nats-agent.
#
# Proves:
# - starts a real nats-server with JetStream enabled
# - starts a controller client using nats-agent-core
# - starts vyos-nats-agent against the same NATS server
# - submits trace action and expects action workflow statuses
# - verifies final action result success with placeholder payload
# - stops agent with SIGINT and verifies graceful shutdown
#
# Usage:
#   ./tests/scripts/phase4-real-nats-action-smoke.sh
#
# Optional environment variables:
#   NATS_PORT=4223                Use a non-default NATS port if 4222 is busy.
#   PRINT_LOGS_ON_PASS=true       Print nats-server / agent / controller logs on success.
#   KEEP_SMOKE_ARTIFACTS=true     Keep temporary files and print their directory path.
#
# Example:
#   PRINT_LOGS_ON_PASS=true KEEP_SMOKE_ARTIFACTS=true NATS_PORT=4223 \
#     ./tests/scripts/phase4-real-nats-action-smoke.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

NATS_PORT="${NATS_PORT:-4222}"
NATS_URL="nats://127.0.0.1:${NATS_PORT}"
PRINT_LOGS_ON_PASS="${PRINT_LOGS_ON_PASS:-false}"      # true|false
KEEP_SMOKE_ARTIFACTS="${KEEP_SMOKE_ARTIFACTS:-false}"  # true|false

mkdir -p "${ROOT_DIR}/.tmp"
WORK_DIR="$(mktemp -d "${ROOT_DIR}/.tmp/vyos-nats-agent-phase4-XXXXXX")"
TMP_CONFIG="${WORK_DIR}/config.yaml"
NATS_LOG="${WORK_DIR}/nats-server.log"
AGENT_LOG="${WORK_DIR}/vyos-nats-agent.log"
CONTROLLER_LOG="${WORK_DIR}/controller.log"
AGENT_BIN="${WORK_DIR}/vyos-nats-agent"
READY_FILE="${WORK_DIR}/controller.ready"
CONTROLLER_DIR="${WORK_DIR}/controller"

NATS_PID=""
AGENT_PID=""
CONTROLLER_PID=""

cleanup() {
  set +e

  if [[ -n "${CONTROLLER_PID}" ]] && kill -0 "${CONTROLLER_PID}" >/dev/null 2>&1; then
    kill "${CONTROLLER_PID}" >/dev/null 2>&1
    wait "${CONTROLLER_PID}" >/dev/null 2>&1
  fi

  if [[ -n "${AGENT_PID}" ]] && kill -0 "${AGENT_PID}" >/dev/null 2>&1; then
    kill -INT "${AGENT_PID}" >/dev/null 2>&1
    sleep 1
    kill "${AGENT_PID}" >/dev/null 2>&1
    wait "${AGENT_PID}" >/dev/null 2>&1
  fi

  if [[ -n "${NATS_PID}" ]] && kill -0 "${NATS_PID}" >/dev/null 2>&1; then
    kill "${NATS_PID}" >/dev/null 2>&1
    wait "${NATS_PID}" >/dev/null 2>&1
  fi

  rm -rf "${CONTROLLER_DIR}"
  if [[ "${KEEP_SMOKE_ARTIFACTS}" != "true" ]]; then
    rm -rf "${WORK_DIR}"
  fi
}
trap cleanup EXIT

fail() {
  echo "[FAIL] $*" >&2
  echo "" >&2
  echo "---- nats-server log ----" >&2
  [[ -f "${NATS_LOG}" ]] && tail -n 140 "${NATS_LOG}" >&2 || true
  echo "" >&2
  echo "---- agent log ----" >&2
  [[ -f "${AGENT_LOG}" ]] && tail -n 220 "${AGENT_LOG}" >&2 || true
  echo "" >&2
  echo "---- controller log ----" >&2
  [[ -f "${CONTROLLER_LOG}" ]] && tail -n 260 "${CONTROLLER_LOG}" >&2 || true
  exit 1
}

print_logs() {
  echo ""
  echo "---- nats-server log ----"
  [[ -f "${NATS_LOG}" ]] && tail -n 220 "${NATS_LOG}" || true
  echo ""
  echo "---- agent log ----"
  [[ -f "${AGENT_LOG}" ]] && tail -n 280 "${AGENT_LOG}" || true
  echo ""
  echo "---- controller log ----"
  [[ -f "${CONTROLLER_LOG}" ]] && tail -n 320 "${CONTROLLER_LOG}" || true
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "required command not found: $1"
  fi
}

wait_for_file() {
  local path="$1"
  local attempts="${2:-80}"

  for _ in $(seq 1 "${attempts}"); do
    if [[ -f "${path}" ]]; then
      return 0
    fi
    sleep 0.2
  done

  return 1
}

port_in_use() {
  local port="$1"
  ss -ltn 2>/dev/null | awk -v p=":${port}" '$1=="LISTEN" && index($4, p)>0 {found=1} END {exit !found}'
}

wait_for_nats_ready() {
  local attempts="${1:-100}"

  for _ in $(seq 1 "${attempts}"); do
    if [[ -n "${NATS_PID}" ]] && ! kill -0 "${NATS_PID}" >/dev/null 2>&1; then
      return 1
    fi
    if [[ -f "${NATS_LOG}" ]] && grep -q "Server is ready" "${NATS_LOG}"; then
      return 0
    fi
    sleep 0.2
  done

  return 1
}

echo "[INFO] checking required commands"
require_cmd go
require_cmd nats-server
require_cmd ss

if port_in_use "${NATS_PORT}"; then
  fail "port ${NATS_PORT} is already in use. Re-run with NATS_PORT=4223 (or any free port)."
fi

echo "[INFO] preparing temporary config at ${TMP_CONFIG}"
sed -e "s#nats://127.0.0.1:4222#${NATS_URL}#g" config.example.yaml > "${TMP_CONFIG}"

echo "[INFO] building vyos-nats-agent"
go build -o "${AGENT_BIN}" ./cmd/vyos-nats-agent

echo "[INFO] starting nats-server with JetStream on ${NATS_URL}"
nats-server -js -p "${NATS_PORT}" -sd "${WORK_DIR}/jetstream" >"${NATS_LOG}" 2>&1 &
NATS_PID="$!"

if ! wait_for_nats_ready 100; then
  fail "nats-server did not become ready"
fi

echo "[INFO] creating temporary controller client"
rm -rf "${CONTROLLER_DIR}"
mkdir -p "${CONTROLLER_DIR}"

cat > "${CONTROLLER_DIR}/main.go" <<'GO'
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/routerarchitects/nats-agent-core/agentcore"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/config"
)

const wireVersion = "1.0"

type actionPayload struct {
	Executor         string `json:"executor"`
	Action           string `json:"action"`
	Target           string `json:"target"`
	Status           string `json:"status"`
	Placeholder      bool   `json:"placeholder"`
	ReceivedPayload  bool   `json:"received_payload"`
}

func main() {
	var configPath string
	var readyFile string
	var timeout time.Duration

	flag.StringVar(&configPath, "config", "", "Path to YAML config file")
	flag.StringVar(&readyFile, "ready-file", "", "Path to create after subscriptions are active")
	flag.DurationVar(&timeout, "timeout", 45*time.Second, "Smoke test timeout")
	flag.Parse()

	if configPath == "" {
		fatalf("missing --config")
	}

	appCfg, err := config.Load(configPath)
	if err != nil {
		fatalf("load config: %v", err)
	}

	coreCfg, err := appCfg.ToAgentCoreConfig()
	if err != nil {
		fatalf("convert config: %v", err)
	}
	coreCfg.AgentName = "vyos-nats-agent-phase4-action-controller"
	coreCfg.NATS.ClientName = "vyos-nats-agent-phase4-action-controller"

	client, err := agentcore.New(coreCfg)
	if err != nil {
		fatalf("create agentcore client: %v", err)
	}

	statusCh := make(chan agentcore.StatusEnvelope, 32)
	resultCh := make(chan agentcore.ResultEnvelope, 16)
	target := appCfg.Agent.Target

	if err := client.RegisterStatusHandler(target, func(ctx context.Context, msg agentcore.StatusEnvelope) error {
		fmt.Printf("[CONTROLLER] status target=%s rpc_id=%s status=%s stage=%s message=%q\n",
			msg.Target, msg.RPCID, msg.Status, msg.Stage, msg.Message)
		select {
		case statusCh <- msg:
		default:
		}
		return nil
	}); err != nil {
		fatalf("register status handler: %v", err)
	}

	if err := client.RegisterResultHandler(target, func(ctx context.Context, msg agentcore.ResultEnvelope) error {
		fmt.Printf("[CONTROLLER] result target=%s command_type=%s action=%s rpc_id=%s result=%s error_code=%s message=%q\n",
			msg.Target, msg.CommandType, msg.Action, msg.RPCID, msg.Result, msg.ErrorCode, msg.Message)
		select {
		case resultCh <- msg:
		default:
		}
		return nil
	}); err != nil {
		fatalf("register result handler: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		fatalf("start controller client: %v", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if err := client.Close(closeCtx); err != nil {
			fmt.Fprintf(os.Stderr, "[CONTROLLER] close client: %v\n", err)
		}
	}()

	if readyFile != "" {
		if err := os.WriteFile(readyFile, []byte("ready\n"), 0o644); err != nil {
			fatalf("write ready file: %v", err)
		}
	}

	waitForStartupStatus(ctx, statusCh)

	rpcID := fmt.Sprintf("phase4-smoke-action-%d", time.Now().UnixNano())
	_, err = client.SubmitAction(ctx, agentcore.ActionCommand{
		Version:     wireVersion,
		RPCID:       rpcID,
		Target:      target,
		CommandType: "action",
		Action:      "trace",
		Payload:     json.RawMessage(`{"probe":"phase4-smoke"}`),
		Timestamp:   time.Now().UTC(),
	})
	if err != nil {
		fatalf("submit trace action: %v", err)
	}
	fmt.Printf("[CONTROLLER] submitted trace action rpc_id=%s\n", rpcID)

	waitForActionStatus(ctx, statusCh, rpcID, "received", "running")
	waitForActionStatus(ctx, statusCh, rpcID, "executing", "running")
	waitForActionStatus(ctx, statusCh, rpcID, "completed", "success")

	result := waitForActionResult(ctx, resultCh, rpcID)
	if result.Result != "success" {
		fatalf("expected success result, got result=%q error_code=%q message=%q", result.Result, result.ErrorCode, result.Message)
	}
	if result.CommandType != "action" || result.Action != "trace" {
		fatalf("unexpected action result metadata command_type=%q action=%q", result.CommandType, result.Action)
	}
	if result.RPCID != rpcID {
		fatalf("rpc id mismatch expected=%q got=%q", rpcID, result.RPCID)
	}

	if !json.Valid(result.Payload) {
		fatalf("result payload is not valid json")
	}
	var payload actionPayload
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		fatalf("decode result payload: %v", err)
	}
	if !payload.Placeholder || payload.Executor != "placeholder_trace" {
		fatalf("unexpected payload placeholder=%v executor=%q", payload.Placeholder, payload.Executor)
	}
	if payload.Action != "trace" || payload.Target != target || payload.Status != "completed" {
		fatalf("unexpected payload action=%q target=%q status=%q", payload.Action, payload.Target, payload.Status)
	}

	fmt.Println("[CONTROLLER] phase4 action smoke flow passed")
}

func waitForStartupStatus(ctx context.Context, ch <-chan agentcore.StatusEnvelope) {
	for {
		select {
		case <-ctx.Done():
			fatalf("timed out waiting for startup status: %v", ctx.Err())
		case msg := <-ch:
			if msg.Status == "running" && msg.Stage == "startup" {
				return
			}
		}
	}
}

func waitForActionStatus(ctx context.Context, ch <-chan agentcore.StatusEnvelope, rpcID, stage, status string) {
	for {
		select {
		case <-ctx.Done():
			fatalf("timed out waiting for action status rpc_id=%s stage=%s status=%s: %v", rpcID, stage, status, ctx.Err())
		case msg := <-ch:
			if msg.RPCID == rpcID && msg.Stage == stage && msg.Status == status {
				return
			}
		}
	}
}

func waitForActionResult(ctx context.Context, ch <-chan agentcore.ResultEnvelope, rpcID string) agentcore.ResultEnvelope {
	for {
		select {
		case <-ctx.Done():
			fatalf("timed out waiting for action result rpc_id=%s: %v", rpcID, ctx.Err())
		case msg := <-ch:
			if msg.RPCID == rpcID && msg.CommandType == "action" && msg.Action == "trace" {
				return msg
			}
		}
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[CONTROLLER][FAIL] "+format+"\n", args...)
	os.Exit(1)
}
GO

echo "[INFO] starting controller client and waiting until subscriptions are active"
go run "${CONTROLLER_DIR}" --config "${TMP_CONFIG}" --ready-file "${READY_FILE}" >"${CONTROLLER_LOG}" 2>&1 &
CONTROLLER_PID="$!"

if ! wait_for_file "${READY_FILE}" 90; then
  fail "controller did not become ready"
fi

echo "[INFO] starting vyos-nats-agent"
"${AGENT_BIN}" --config "${TMP_CONFIG}" >"${AGENT_LOG}" 2>&1 &
AGENT_PID="$!"

if ! kill -0 "${AGENT_PID}" >/dev/null 2>&1; then
  fail "vyos-nats-agent exited immediately"
fi

echo "[INFO] waiting for controller smoke flow to finish"
if ! wait "${CONTROLLER_PID}"; then
  fail "controller smoke flow failed"
fi
CONTROLLER_PID=""

echo "[INFO] stopping vyos-nats-agent with SIGINT"
kill -INT "${AGENT_PID}" >/dev/null 2>&1 || true

for _ in $(seq 1 60); do
  if ! kill -0 "${AGENT_PID}" >/dev/null 2>&1; then
    AGENT_PID=""
    break
  fi
  sleep 0.2
done

if [[ -n "${AGENT_PID}" ]]; then
  fail "vyos-nats-agent did not exit after SIGINT"
fi

echo "[PASS] Phase 4 real-NATS action smoke test passed"

if [[ "${PRINT_LOGS_ON_PASS}" == "true" ]]; then
  print_logs
fi

if [[ "${KEEP_SMOKE_ARTIFACTS}" == "true" ]]; then
  echo "[INFO] kept smoke artifacts at ${WORK_DIR}"
fi
