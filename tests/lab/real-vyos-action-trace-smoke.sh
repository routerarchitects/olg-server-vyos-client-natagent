#!/usr/bin/env bash
set -euo pipefail

# Real-VyOS trace action smoke helper.
#
# This script is lab-only. It assumes NATS and vyos-nats-agent are already
# running and focuses on trace action submission, verification, and evidence
# collection.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

usage() {
  cat <<'EOF'
Usage:
  ./tests/lab/real-vyos-action-trace-smoke.sh
  ./tests/lab/real-vyos-action-trace-smoke.sh --help

Purpose:
  Run the Phase 9 real VyOS trace action smoke through the real NATS path,
  start a local HTTP server to receive the uploaded PCAP, verify the PCAP header,
  and write status/result/summary evidence files.

Manual prerequisites:
  1. Start NATS manually on the Ubuntu host, for example:
       nats-server -js -p 4222
  2. Install/start vyos-nats-agent manually inside the VyOS VM, for example:
       sudo install -m 0755 ~/vyos-nats-agent /usr/local/bin/vyos-nats-agent
       nohup /usr/local/bin/vyos-nats-agent --config ~/vyos-nats-agent.yaml >/tmp/vyos-nats-agent.log 2>&1 &

Required environment:
  REAL_VYOS_LAB_ACK=I_UNDERSTAND
  NATS_URL=nats://<host>:4222
  VYOS_TARGET=vyos
  VYOS_HOST=<host-or-ip>
  VYOS_USER=vyos
  Set exactly one of:
    VYOS_PASSWORD=<password>
    VYOS_SSH_KEY=/path/to/private/key
  STATE_PATH=/tmp/vyos-nats-agent/state.json

Optional environment:
  INTERFACE=eth0
  DURATION=5
  PACKETS=20
  ARTIFACT_DIR=tests/lab/artifacts/manual-run
  RPC_ID=real-vyos-trace-<timestamp>
  TIMEOUT=120s
  UPLOAD_HOST=<override-local-controller-ip>
  REMOTE_AGENT_LOG=/tmp/vyos-nats-agent.log
  KEEP_WORK_DIR=false
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "[FAIL] unknown argument: $1" >&2
      echo >&2
      usage >&2
      exit 1
      ;;
  esac
done

REAL_VYOS_LAB_ACK="${REAL_VYOS_LAB_ACK:-}"
NATS_URL="${NATS_URL:-}"
VYOS_TARGET="${VYOS_TARGET:-vyos}"
TARGET="${TARGET:-${VYOS_TARGET}}"
VYOS_HOST="${VYOS_HOST:-}"
VYOS_USER="${VYOS_USER:-}"
VYOS_PASSWORD="${VYOS_PASSWORD:-}"
VYOS_SSH_KEY="${VYOS_SSH_KEY:-}"
STATE_PATH="${STATE_PATH:-}"
INTERFACE="${INTERFACE:-eth0}"
DURATION="${DURATION:-5}"
PACKETS="${PACKETS:-20}"
ARTIFACT_DIR="${ARTIFACT_DIR:-${ROOT_DIR}/tests/lab/artifacts/real-vyos-action-trace-$(date +%Y%m%dT%H%M%SZ)}"
RPC_ID="${RPC_ID:-real-vyos-trace-$(date +%s)-$$}"
TIMEOUT="${TIMEOUT:-120s}"
UPLOAD_HOST="${UPLOAD_HOST:-}"
REMOTE_AGENT_LOG="${REMOTE_AGENT_LOG:-/tmp/vyos-nats-agent.log}"
KEEP_WORK_DIR="${KEEP_WORK_DIR:-false}"

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/vyos-nats-agent-real-trace-lab-XXXXXX")"
TMP_CONFIG="${WORK_DIR}/controller-config.yaml"
CONTROLLER_DIR="${WORK_DIR}/controller"
CONTROLLER_LOG="${ARTIFACT_DIR}/controller.log"
AGENT_LOG="${ARTIFACT_DIR}/agent.log"

cleanup() {
  set +e
  if [[ "${KEEP_WORK_DIR}" != "true" ]]; then
    rm -rf "${WORK_DIR}"
  else
    echo "[INFO] kept lab work dir at ${WORK_DIR}"
  fi
}
trap cleanup EXIT

fail() {
  collect_remote_agent_log_best_effort
  echo "[FAIL] $*" >&2
  echo "" >&2
  echo "Artifacts: ${ARTIFACT_DIR}" >&2
  echo "" >&2
  echo "---- controller log ----" >&2
  [[ -f "${CONTROLLER_LOG}" ]] && tail -n 260 "${CONTROLLER_LOG}" >&2 || true
  echo "" >&2
  echo "---- agent log ----" >&2
  [[ -f "${AGENT_LOG}" ]] && tail -n 260 "${AGENT_LOG}" >&2 || true
  exit 1
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "required command not found: $1"
  fi
}

record_command() {
  printf '%s\n' "$*" >> "${ARTIFACT_DIR}/commands-run.txt"
}

write_environment_summary() {
  {
    echo "real_vyos_lab_ack_set=$([[ "${REAL_VYOS_LAB_ACK}" == "I_UNDERSTAND" ]] && echo true || echo false)"
    echo "nats_url_set=$([[ -n "${NATS_URL}" ]] && echo true || echo false)"
    echo "vyos_target=${TARGET}"
    echo "vyos_host_set=$([[ -n "${VYOS_HOST}" ]] && echo true || echo false)"
    echo "vyos_user_set=$([[ -n "${VYOS_USER}" ]] && echo true || echo false)"
    echo "vyos_password_set=$([[ -n "${VYOS_PASSWORD}" ]] && echo true || echo false)"
    echo "vyos_ssh_key_set=$([[ -n "${VYOS_SSH_KEY}" ]] && echo true || echo false)"
    echo "state_path=${STATE_PATH}"
    echo "interface=${INTERFACE}"
    echo "duration=${DURATION}"
    echo "packets=${PACKETS}"
    echo "artifact_dir=${ARTIFACT_DIR}"
    echo "rpc_id=${RPC_ID}"
    echo "remote_agent_log=${REMOTE_AGENT_LOG}"
  } > "${ARTIFACT_DIR}/environment-summary.txt"
}

ssh_cmd() {
  local remote_cmd="$1"
  local -a base_args=(-o StrictHostKeyChecking=accept-new -o UserKnownHostsFile="${ARTIFACT_DIR}/known_hosts")

  record_command "ssh ${VYOS_USER}@${VYOS_HOST} '${remote_cmd}'"

  if [[ -n "${VYOS_SSH_KEY}" ]]; then
    ssh "${base_args[@]}" -i "${VYOS_SSH_KEY}" "${VYOS_USER}@${VYOS_HOST}" "${remote_cmd}"
    return
  fi

  SSHPASS="${VYOS_PASSWORD}" sshpass -e ssh "${base_args[@]}" "${VYOS_USER}@${VYOS_HOST}" "${remote_cmd}"
}

validate_no_single_quotes() {
  local name="$1"
  local value="$2"
  if [[ "${value}" == *"'"* ]]; then
    fail "${name} must not contain single quotes"
  fi
}

copy_state_artifact() {
  local out="$1"
  if ! ssh_cmd "cat '${STATE_PATH}'" > "${out}" 2>>"${ARTIFACT_DIR}/ssh-errors.log"; then
    fail "could not read state file from VyOS host at STATE_PATH"
  fi
}

collect_remote_agent_log_best_effort() {
  mkdir -p "${ARTIFACT_DIR}" >/dev/null 2>&1 || true
  if ! ssh_cmd "cat '${REMOTE_AGENT_LOG}'" > "${AGENT_LOG}" 2>>"${ARTIFACT_DIR}/ssh-errors.log"; then
    echo "[WARN] could not collect remote agent log from ${REMOTE_AGENT_LOG}" >&2
  fi
}

if [[ "${REAL_VYOS_LAB_ACK}" != "I_UNDERSTAND" ]]; then
  fail "refusing to run real VyOS trace action smoke without REAL_VYOS_LAB_ACK=I_UNDERSTAND"
fi
if [[ -z "${NATS_URL}" ]]; then
  fail "NATS_URL is required"
fi
if [[ -z "${VYOS_HOST}" ]]; then
  fail "VYOS_HOST is required"
fi
if [[ -z "${VYOS_USER}" ]]; then
  fail "VYOS_USER is required"
fi
if [[ -z "${VYOS_PASSWORD}" && -z "${VYOS_SSH_KEY}" ]]; then
  fail "VYOS_PASSWORD or VYOS_SSH_KEY is required"
fi
if [[ -n "${VYOS_PASSWORD}" && -n "${VYOS_SSH_KEY}" ]]; then
  fail "set only one of VYOS_PASSWORD or VYOS_SSH_KEY"
fi
if [[ -z "${STATE_PATH}" ]]; then
  fail "STATE_PATH is required"
fi

validate_no_single_quotes "STATE_PATH" "${STATE_PATH}"
validate_no_single_quotes "REMOTE_AGENT_LOG" "${REMOTE_AGENT_LOG}"

require_cmd go
require_cmd ssh
if [[ -n "${VYOS_PASSWORD}" ]]; then
  require_cmd sshpass
fi

mkdir -p "${ARTIFACT_DIR}" "${CONTROLLER_DIR}"
: > "${ARTIFACT_DIR}/commands-run.txt"
: > "${AGENT_LOG}"
write_environment_summary

echo "[INFO] artifacts will be written to ${ARTIFACT_DIR}"
echo "[INFO] target=${TARGET} rpc_id=${RPC_ID}"
echo "[INFO] assuming NATS server is already running at ${NATS_URL}"
echo "[INFO] assuming vyos-nats-agent is already running on ${VYOS_HOST}"

echo "[INFO] preparing controller config"
sed \
  -e "s#nats://127.0.0.1:4222#${NATS_URL}#g" \
  -e "s#target: vyos#target: ${TARGET}#g" \
  config.example.yaml > "${TMP_CONFIG}"

cat > "${CONTROLLER_DIR}/main.go" <<'GO'
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/routerarchitects/nats-agent-core/agentcore"
	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/config"
)

func main() {
	var configPath string
	var iface string
	var duration int
	var packets int
	var rpcID string
	var artifactDir string
	var timeout time.Duration
	var vyosHost string
	var uploadHost string

	flag.StringVar(&configPath, "config", "", "Path to controller YAML config")
	flag.StringVar(&iface, "interface", "eth0", "Interface to trace")
	flag.IntVar(&duration, "duration", 5, "Trace duration in seconds")
	flag.IntVar(&packets, "packets", 20, "Trace packet limit")
	flag.StringVar(&rpcID, "rpc-id", "", "RPC ID")
	flag.StringVar(&artifactDir, "artifact-dir", "", "Artifact directory")
	flag.DurationVar(&timeout, "timeout", 120*time.Second, "Timeout")
	flag.StringVar(&vyosHost, "vyos-host", "", "VyOS VM host address")
	flag.StringVar(&uploadHost, "upload-host", "", "Upload server host address override")
	flag.Parse()

	if configPath == "" || rpcID == "" || artifactDir == "" {
		fatalf("missing required flags")
	}

	appCfg, err := config.Load(configPath)
	if err != nil {
		fatalf("load config: %v", err)
	}
	coreCfg, err := appCfg.ToAgentCoreConfig()
	if err != nil {
		fatalf("convert config: %v", err)
	}
	coreCfg.AgentName = "vyos-nats-agent-real-lab-trace-controller"
	coreCfg.NATS.ClientName = "vyos-nats-agent-real-lab-trace-controller"

	client, err := agentcore.New(coreCfg)
	if err != nil {
		fatalf("create agentcore client: %v", err)
	}

	// Spin up local HTTP upload server
	uploadIP := uploadHost
	if uploadIP == "" {
		if vyosHost != "" && vyosHost != "127.0.0.1" && vyosHost != "localhost" {
			conn, err := net.Dial("udp", vyosHost+":1")
			if err == nil {
				uploadIP = conn.LocalAddr().(*net.UDPAddr).IP.String()
				conn.Close()
			}
		}
		if uploadIP == "" {
			uploadIP = getFirstNonLoopbackIP()
		}
	}

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		fatalf("listen: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	uploadURL := fmt.Sprintf("http://%s:%d/upload", uploadIP, port)
	fmt.Printf("[CONTROLLER] HTTP upload server listening, reachable at %s\n", uploadURL)

	uploadDone := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "missing file parameter", http.StatusBadRequest)
			uploadDone <- fmt.Errorf("form file: %w", err)
			return
		}
		defer file.Close()

		pcapData, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "read error", http.StatusInternalServerError)
			uploadDone <- fmt.Errorf("read file: %w", err)
			return
		}

		// Verify PCAP magic number
		if !isValidPCAP(pcapData) {
			http.Error(w, "invalid pcap header magic", http.StatusBadRequest)
			hexLen := 4
			if len(pcapData) < 4 {
				hexLen = len(pcapData)
			}
			uploadDone <- fmt.Errorf("invalid pcap magic header: got %x", pcapData[:hexLen])
			return
		}

		destPath := filepath.Join(artifactDir, "captured.pcap")
		if err := os.WriteFile(destPath, pcapData, 0o644); err != nil {
			http.Error(w, "write error", http.StatusInternalServerError)
			uploadDone <- fmt.Errorf("write file: %w", err)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
		uploadDone <- nil
	})

	server := &http.Server{
		Handler: mux,
	}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[CONTROLLER] http server error: %v\n", err)
		}
	}()
	defer server.Close()

	statusFile, err := os.OpenFile(filepath.Join(artifactDir, "action-status.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		fatalf("open status file: %v", err)
	}
	defer statusFile.Close()

	resultFile, err := os.OpenFile(filepath.Join(artifactDir, "action-result.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		fatalf("open result file: %v", err)
	}
	defer resultFile.Close()

	target := appCfg.Agent.Target
	statusCh := make(chan agentcore.StatusEnvelope, 64)
	resultCh := make(chan agentcore.ResultEnvelope, 32)

	if err := client.RegisterStatusHandler(target, func(ctx context.Context, msg agentcore.StatusEnvelope) error {
		_ = json.NewEncoder(statusFile).Encode(msg)
		fmt.Printf("[CONTROLLER] status target=%s rpc_id=%s uuid=%s status=%s stage=%s message=%q\n",
			msg.Target, msg.RPCID, msg.UUID, msg.Status, msg.Stage, msg.Message)
		select {
		case statusCh <- msg:
		default:
		}
		return nil
	}); err != nil {
		fatalf("register status handler: %v", err)
	}

	if err := client.RegisterResultHandler(target, func(ctx context.Context, msg agentcore.ResultEnvelope) error {
		_ = json.NewEncoder(resultFile).Encode(msg)
		fmt.Printf("[CONTROLLER] result target=%s rpc_id=%s uuid=%s result=%s error_code=%s message=%q\n",
			msg.Target, msg.RPCID, msg.UUID, msg.Result, msg.ErrorCode, msg.Message)
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
		_ = client.Close(closeCtx)
	}()

	payloadMap := map[string]any{
		"interface": iface,
		"duration":  duration,
		"packets":   packets,
		"uri":       uploadURL,
	}
	payloadBytes, err := json.Marshal(payloadMap)
	if err != nil {
		fatalf("marshal payload: %v", err)
	}

	ack, err := client.SubmitAction(ctx, agentcore.ActionCommand{
		Version:     "1.0",
		RPCID:       rpcID,
		Target:      target,
		CommandType: "action",
		Action:      "trace",
		Payload:     payloadBytes,
		Timestamp:   time.Now().UTC(),
	})
	if err != nil {
		fatalf("submit action command: %v", err)
	}
	fmt.Printf("[CONTROLLER] action submitted: accepted=%v subject=%s\n", ack.Accepted, ack.Subject)

	var gotCompletedStatus bool
	var gotSuccessResult bool
	var uploadCompleted bool

	for {
		select {
		case <-ctx.Done():
			fatalf("timeout waiting for action trace completion: %v", ctx.Err())
		case err := <-uploadDone:
			if err != nil {
				fatalf("upload failed: %v", err)
			}
			fmt.Printf("[CONTROLLER] pcap file successfully uploaded and verified\n")
			uploadCompleted = true
			if gotCompletedStatus && gotSuccessResult && uploadCompleted {
				return
			}
		case msg := <-statusCh:
			if msg.RPCID == rpcID {
				if msg.Status == "failure" {
					fatalf("action failed status: %q", msg.Message)
				}
				if msg.Stage == "completed" {
					gotCompletedStatus = true
				}
			}
			if gotCompletedStatus && gotSuccessResult && uploadCompleted {
				return
			}
		case msg := <-resultCh:
			if msg.RPCID == rpcID && msg.CommandType == "action" {
				if msg.Result != "success" {
					fatalf("action failed result: error_code=%s message=%q", msg.ErrorCode, msg.Message)
				}
				fmt.Printf("[CONTROLLER] result success: %s\n", msg.Message)
				gotSuccessResult = true
			}
			if gotCompletedStatus && gotSuccessResult && uploadCompleted {
				return
			}
		}
	}
}

func getFirstNonLoopbackIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

func isValidPCAP(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	magic := binary.BigEndian.Uint32(data[:4])
	magicLE := binary.LittleEndian.Uint32(data[:4])
	return magic == 0xa1b2c3d4 || magic == 0xd4c3b2a1 || magic == 0xa1b23c4d || magic == 0x4d3cb2a1 ||
		magicLE == 0xa1b2c3d4 || magicLE == 0xd4c3b2a1 || magicLE == 0xa1b23c4d || magicLE == 0x4d3cb2a1
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[CONTROLLER][FAIL] "+format+"\n", args...)
	os.Exit(1)
}
GO

echo "[INFO] submitting real action trace through NATS"
record_command "go run controller --config <tmp> --interface ${INTERFACE} --duration ${DURATION} --packets ${PACKETS} --rpc-id ${RPC_ID}"
if ! go run "${CONTROLLER_DIR}" \
  --config "${TMP_CONFIG}" \
  --interface "${INTERFACE}" \
  --duration "${DURATION}" \
  --packets "${PACKETS}" \
  --rpc-id "${RPC_ID}" \
  --artifact-dir "${ARTIFACT_DIR}" \
  --timeout "${TIMEOUT}" \
  --vyos-host "${VYOS_HOST}" \
  --upload-host "${UPLOAD_HOST}" >"${CONTROLLER_LOG}" 2>&1; then
  fail "real VyOS action trace smoke failed"
fi

echo "[INFO] collecting state file from VyOS host"
copy_state_artifact "${ARTIFACT_DIR}/state.json"

echo "[INFO] collecting remote agent log from ${REMOTE_AGENT_LOG}"
collect_remote_agent_log_best_effort

cat > "${ARTIFACT_DIR}/phase9-summary.md" <<EOF
# Real VyOS Action Trace Smoke Summary

- Target: ${TARGET}
- RPC ID: ${RPC_ID}
- Interface: ${INTERFACE}
- Duration: ${DURATION}s
- Packets: ${PACKETS}
- State path: ${STATE_PATH}
- Remote agent log: ${REMOTE_AGENT_LOG}
- Result: passed

Evidence files (captured.pcap, action-status.jsonl, action-result.jsonl) successfully written.
EOF

echo "[PASS] Real VyOS action trace lab smoke passed"
echo "[INFO] evidence artifacts: ${ARTIFACT_DIR}"
