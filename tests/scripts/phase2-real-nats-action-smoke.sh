#!/usr/bin/env bash
set -euo pipefail

# Phase 2 real-NATS action smoke test for vyos-nats-agent.
#
# Proves:
# - starts a real nats-server with JetStream enabled
# - starts a controller client using nats-agent-core
# - starts vyos-nats-agent against the same NATS server
# - observes startup status on status.vyos
# - sends a trace action using nats-agent-core SubmitAction
# - observes the Phase 2 placeholder result on result.vyos
# - sends SIGINT to the agent and verifies graceful shutdown
#
# Requirements:
# - go
# - nats-server
#
# Usage:
#   chmod +x tests/scripts/phase2-real-nats-action-smoke.sh
#   tests/scripts/phase2-real-nats-action-smoke.sh
#
# Optional:
#   NATS_PORT=4223 tests/scripts/phase2-real-nats-action-smoke.sh
#   NATS_KILL_WITH_SUDO=always tests/scripts/phase2-real-nats-action-smoke.sh
#   PRINT_LOGS_ON_PASS=true KEEP_SMOKE_ARTIFACTS=true tests/scripts/phase2-real-nats-action-smoke.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

NATS_PORT="${NATS_PORT:-4222}"
NATS_URL="nats://127.0.0.1:${NATS_PORT}"
NATS_KILL_WITH_SUDO="${NATS_KILL_WITH_SUDO:-auto}" # auto|always|never
PRINT_LOGS_ON_PASS="${PRINT_LOGS_ON_PASS:-false}"   # true|false
KEEP_SMOKE_ARTIFACTS="${KEEP_SMOKE_ARTIFACTS:-false}" # true|false

WORK_DIR="$(mktemp -d -t vyos-nats-agent-phase2-XXXXXX)"
TMP_CONFIG="${WORK_DIR}/config.yaml"
NATS_LOG="${WORK_DIR}/nats-server.log"
AGENT_LOG="${WORK_DIR}/vyos-nats-agent.log"
CONTROLLER_LOG="${WORK_DIR}/controller.log"
AGENT_BIN="${WORK_DIR}/vyos-nats-agent"
READY_FILE="${WORK_DIR}/controller.ready"
CONTROLLER_DIR="${ROOT_DIR}/.tmp/phase2-smoke-controller"

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
  [[ -f "${NATS_LOG}" ]] && tail -n 120 "${NATS_LOG}" >&2 || true
  echo "" >&2
  echo "---- agent log ----" >&2
  [[ -f "${AGENT_LOG}" ]] && tail -n 160 "${AGENT_LOG}" >&2 || true
  echo "" >&2
  echo "---- controller log ----" >&2
  [[ -f "${CONTROLLER_LOG}" ]] && tail -n 160 "${CONTROLLER_LOG}" >&2 || true
  exit 1
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "required command not found: $1"
  fi
}

print_logs() {
  echo ""
  echo "---- nats-server log ----"
  [[ -f "${NATS_LOG}" ]] && tail -n 200 "${NATS_LOG}" || true
  echo ""
  echo "---- agent log ----"
  [[ -f "${AGENT_LOG}" ]] && tail -n 240 "${AGENT_LOG}" || true
  echo ""
  echo "---- controller log ----"
  [[ -f "${CONTROLLER_LOG}" ]] && tail -n 240 "${CONTROLLER_LOG}" || true
}

wait_for_tcp() {
  local host="$1"
  local port="$2"
  local attempts="${3:-75}"

  for _ in $(seq 1 "${attempts}"); do
    if (echo >"/dev/tcp/${host}/${port}") >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done

  return 1
}

is_tcp_open() {
  local host="$1"
  local port="$2"
  (echo >"/dev/tcp/${host}/${port}") >/dev/null 2>&1
}

wait_for_port_closed() {
  local host="$1"
  local port="$2"
  local attempts="${3:-50}"

  for _ in $(seq 1 "${attempts}"); do
    if ! is_tcp_open "${host}" "${port}"; then
      return 0
    fi
    sleep 0.2
  done

  return 1
}

wait_for_file() {
  local path="$1"
  local attempts="${2:-75}"

  for _ in $(seq 1 "${attempts}"); do
    if [[ -f "${path}" ]]; then
      return 0
    fi
    sleep 0.2
  done

  return 1
}

send_signal_to_pid() {
  local pid="$1"
  local sig="$2"

  if kill -"${sig}" "${pid}" >/dev/null 2>&1; then
    return 0
  fi

  if [[ "${NATS_KILL_WITH_SUDO}" == "never" ]]; then
    return 1
  fi
  if ! command -v sudo >/dev/null 2>&1; then
    return 1
  fi

  # Try password-less sudo first.
  if sudo -n kill -"${sig}" "${pid}" >/dev/null 2>&1; then
    return 0
  fi

  # In always mode, or interactive auto mode, allow sudo prompt.
  if [[ "${NATS_KILL_WITH_SUDO}" == "always" ]] || { [[ "${NATS_KILL_WITH_SUDO}" == "auto" ]] && [[ -t 0 ]]; }; then
    echo "[INFO] sudo is required to send SIG${sig} to pid=${pid}; you may be prompted for password"
    sudo kill -"${sig}" "${pid}" >/dev/null 2>&1
    return $?
  fi

  return 1
}

detect_listener() {
  local port="$1"
  local pid=""
  local name=""

  if command -v lsof >/dev/null 2>&1; then
    pid="$(lsof -nP -iTCP:"${port}" -sTCP:LISTEN -t 2>/dev/null | head -n1 || true)"
    if [[ -n "${pid}" ]] && [[ -r "/proc/${pid}/comm" ]]; then
      name="$(cat "/proc/${pid}/comm" 2>/dev/null || true)"
    fi
  fi

  if [[ -z "${pid}" ]] && command -v ss >/dev/null 2>&1; then
    local line
    line="$(ss -ltnp "sport = :${port}" 2>/dev/null | awk '/LISTEN/ {print; exit}' || true)"
    if [[ -z "${line}" ]]; then
      line="$(ss -ltnp 2>/dev/null | awk -v p=":${port}" '$1=="LISTEN" && index($4, p)>0 {print; exit}' || true)"
    fi
    if [[ -n "${line}" ]]; then
      pid="$(echo "${line}" | sed -n 's/.*pid=\([0-9]\+\).*/\1/p' | head -n1)"
      name="$(echo "${line}" | sed -n 's/.*users:(("\([^"]\+\)".*/\1/p' | head -n1)"
      if [[ -z "${name}" ]] && [[ -n "${pid}" ]] && [[ -r "/proc/${pid}/comm" ]]; then
        name="$(cat "/proc/${pid}/comm" 2>/dev/null || true)"
      fi
    fi
  fi

  if [[ -z "${pid}" ]] && command -v fuser >/dev/null 2>&1; then
    pid="$(fuser -n tcp "${port}" 2>/dev/null | tr ' ' '\n' | sed '/^$/d' | head -n1 || true)"
    if [[ -n "${pid}" ]] && [[ -r "/proc/${pid}/comm" ]]; then
      name="$(cat "/proc/${pid}/comm" 2>/dev/null || true)"
    fi
  fi

  echo "${pid}|${name}"
}

ensure_nats_port_available() {
  local port="$1"
  local info pid name

  if ! is_tcp_open "127.0.0.1" "${port}"; then
    return 0
  fi

  info="$(detect_listener "${port}")"
  pid="${info%%|*}"
  name="${info#*|}"

  if [[ -z "${pid}" ]]; then
    local pids=""
    pids="$(pgrep -x nats-server 2>/dev/null || true)"
    if [[ -n "${pids}" ]]; then
      echo "[WARN] port ${port} is open, owner PID is not visible; trying to stop discovered nats-server process(es): ${pids//$'\n'/, }"
      while IFS= read -r npid; do
        [[ -z "${npid}" ]] && continue

        local args=""
        args="$(ps -p "${npid}" -o args= 2>/dev/null || true)"
        # Candidate rules:
        # - explicit -p <port>
        # - no explicit -p and script is using default port 4222
        if [[ "${args}" =~ (^|[[:space:]])-p[[:space:]]*${port}([[:space:]]|$) ]] || { [[ "${port}" == "4222" ]] && [[ ! "${args}" =~ (^|[[:space:]])-p[[:space:]]*[0-9]+ ]]; }; then
          send_signal_to_pid "${npid}" TERM || true
        fi
      done <<< "${pids}"

      if wait_for_port_closed "127.0.0.1" "${port}" 40; then
        echo "[INFO] previous nats-server stopped (fallback detection)"
        return 0
      fi

      while IFS= read -r npid; do
        [[ -z "${npid}" ]] && continue
        local args=""
        args="$(ps -p "${npid}" -o args= 2>/dev/null || true)"
        if [[ "${args}" =~ (^|[[:space:]])-p[[:space:]]*${port}([[:space:]]|$) ]] || { [[ "${port}" == "4222" ]] && [[ ! "${args}" =~ (^|[[:space:]])-p[[:space:]]*[0-9]+ ]]; }; then
          send_signal_to_pid "${npid}" KILL || true
        fi
      done <<< "${pids}"

      if wait_for_port_closed "127.0.0.1" "${port}" 20; then
        echo "[INFO] previous nats-server force-stopped (fallback detection)"
        return 0
      fi
    fi

    fail "port ${port} is already open, but owning process could not be identified or stopped. Try: NATS_PORT=4223 ./tests/scripts/phase2-real-nats-action-smoke.sh or stop existing nats-server manually."
  fi

  if [[ "${name}" != "nats-server" ]]; then
    fail "port ${port} is already in use by pid=${pid} process=${name:-unknown}; refusing to stop non-nats process. Set NATS_PORT to a free port."
  fi

  echo "[INFO] found existing nats-server on port ${port} (pid=${pid}); stopping it"
  send_signal_to_pid "${pid}" TERM || true

  if wait_for_port_closed "127.0.0.1" "${port}" 40; then
    echo "[INFO] previous nats-server stopped"
    return 0
  fi

  echo "[WARN] existing nats-server did not stop after SIGTERM; sending SIGKILL"
  send_signal_to_pid "${pid}" KILL || true

  if wait_for_port_closed "127.0.0.1" "${port}" 20; then
    echo "[INFO] previous nats-server force-stopped"
    return 0
  fi

  fail "failed to stop existing nats-server pid=${pid} on port ${port}. If this process is root-owned, rerun with NATS_KILL_WITH_SUDO=always (or stop it manually)."
}

echo "[INFO] checking required commands"
require_cmd go
require_cmd nats-server

echo "[INFO] ensuring NATS port ${NATS_PORT} is available"
ensure_nats_port_available "${NATS_PORT}"

echo "[INFO] preparing temporary config at ${TMP_CONFIG}"
sed "s#nats://127.0.0.1:4222#${NATS_URL}#g" config.example.yaml > "${TMP_CONFIG}"

echo "[INFO] building vyos-nats-agent"
go build -o "${AGENT_BIN}" ./cmd/vyos-nats-agent

echo "[INFO] starting nats-server with JetStream on ${NATS_URL}"
nats-server -js -p "${NATS_PORT}" -sd "${WORK_DIR}/jetstream" >"${NATS_LOG}" 2>&1 &
NATS_PID="$!"

for _ in $(seq 1 75); do
  if ! kill -0 "${NATS_PID}" >/dev/null 2>&1; then
    fail "nats-server exited before becoming ready"
  fi
  if is_tcp_open "127.0.0.1" "${NATS_PORT}"; then
    break
  fi
  sleep 0.2
done

if ! is_tcp_open "127.0.0.1" "${NATS_PORT}"; then
  fail "nats-server did not open port ${NATS_PORT}"
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

func main() {
	var configPath string
	var readyFile string
	var timeout time.Duration

	flag.StringVar(&configPath, "config", "", "Path to YAML config file")
	flag.StringVar(&readyFile, "ready-file", "", "Path to create after subscriptions are active")
	flag.DurationVar(&timeout, "timeout", 30*time.Second, "Smoke test timeout")
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

	coreCfg.AgentName = "vyos-nats-agent-smoke-controller"
	coreCfg.NATS.ClientName = "vyos-nats-agent-smoke-controller"

	client, err := agentcore.New(coreCfg)
	if err != nil {
		fatalf("create agentcore client: %v", err)
	}

	statusCh := make(chan agentcore.StatusEnvelope, 8)
	resultCh := make(chan agentcore.ResultEnvelope, 8)

	target := appCfg.Agent.Target

	if err := client.RegisterStatusHandler(target, func(ctx context.Context, msg agentcore.StatusEnvelope) error {
		fmt.Printf("[CONTROLLER] status target=%s status=%s stage=%s message=%q rpc_id=%s uuid=%s\n",
			msg.Target, msg.Status, msg.Stage, msg.Message, msg.RPCID, msg.UUID)
		select {
		case statusCh <- msg:
		default:
		}
		return nil
	}); err != nil {
		fatalf("register status handler: %v", err)
	}

	if err := client.RegisterResultHandler(target, func(ctx context.Context, msg agentcore.ResultEnvelope) error {
		fmt.Printf("[CONTROLLER] result target=%s command_type=%s action=%s result=%s error_code=%s message=%q rpc_id=%s uuid=%s\n",
			msg.Target, msg.CommandType, msg.Action, msg.Result, msg.ErrorCode, msg.Message, msg.RPCID, msg.UUID)
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

	startupStatus := waitForStartupStatus(ctx, statusCh)
	fmt.Printf("[CONTROLLER] observed startup status: status=%s stage=%s\n", startupStatus.Status, startupStatus.Stage)

	rpcID := fmt.Sprintf("phase2-smoke-action-%d", time.Now().UnixNano())
	_, err = client.SubmitAction(ctx, agentcore.ActionCommand{
		Version:     wireVersion,
		RPCID:       rpcID,
		Target:      target,
		CommandType: "action",
		Action:      "trace",
		Payload:     json.RawMessage(`{"probe":"phase2-smoke"}`),
		Timestamp:   time.Now().UTC(),
	})
	if err != nil {
		fatalf("submit trace action: %v", err)
	}
	fmt.Printf("[CONTROLLER] submitted trace action rpc_id=%s\n", rpcID)

	result := waitForResult(ctx, resultCh, rpcID)
	if result.Result != "failure" || result.ErrorCode != "not_implemented" {
		fatalf("unexpected phase2 action result: result=%q error_code=%q message=%q", result.Result, result.ErrorCode, result.Message)
	}

	fmt.Println("[CONTROLLER] phase2 controller smoke test passed")
}

func waitForStartupStatus(ctx context.Context, ch <-chan agentcore.StatusEnvelope) agentcore.StatusEnvelope {
	for {
		select {
		case <-ctx.Done():
			fatalf("timed out waiting for startup status: %v", ctx.Err())
		case msg := <-ch:
			if msg.Status == "running" && msg.Stage == "startup" {
				return msg
			}
		}
	}
}

func waitForResult(ctx context.Context, ch <-chan agentcore.ResultEnvelope, rpcID string) agentcore.ResultEnvelope {
	for {
		select {
		case <-ctx.Done():
			fatalf("timed out waiting for result rpc_id=%s: %v", rpcID, ctx.Err())
		case msg := <-ch:
			if msg.RPCID == rpcID {
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

if ! wait_for_file "${READY_FILE}" 75; then
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

for _ in $(seq 1 50); do
  if ! kill -0 "${AGENT_PID}" >/dev/null 2>&1; then
    AGENT_PID=""
    break
  fi
  sleep 0.2
done

if [[ -n "${AGENT_PID}" ]]; then
  fail "vyos-nats-agent did not exit after SIGINT"
fi

echo "[PASS] Phase 2 real-NATS action smoke test passed"

if [[ "${PRINT_LOGS_ON_PASS}" == "true" ]]; then
  print_logs
fi

if [[ "${KEEP_SMOKE_ARTIFACTS}" == "true" ]]; then
  echo "[INFO] kept smoke artifacts at ${WORK_DIR}"
fi
