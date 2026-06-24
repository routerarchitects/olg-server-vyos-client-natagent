package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/routerarchitects/nats-agent-core/agentcore"
)

const (
	maxDuration = 300
	maxPackets  = 10000
)

var (
	vyosInterfaceRegex = regexp.MustCompile(`^(eth|bond|dum|vlan|wlan|lo)[0-9]+(\.[0-9]+)?$`)
)

type Command interface {
	Run() error
}

type CommandRunner interface {
	Command(ctx context.Context, name string, args ...string) Command
}

type realCommand struct {
	cmd *exec.Cmd
}

func (c *realCommand) Run() error {
	return c.cmd.Run()
}

type RealCommandRunner struct{}

func NewRealCommandRunner() CommandRunner {
	return &RealCommandRunner{}
}

func (r *RealCommandRunner) Command(ctx context.Context, name string, args ...string) Command {
	return &realCommand{
		cmd: exec.CommandContext(ctx, name, args...),
	}
}

type VyOSTraceExecutor struct {
	runner CommandRunner
	client *http.Client
}

func NewVyOSTraceExecutor(runner CommandRunner, client *http.Client) *VyOSTraceExecutor {
	if client == nil {
		client = http.DefaultClient
	}
	return &VyOSTraceExecutor{
		runner: runner,
		client: client,
	}
}

type tracePayload struct {
	Interface string `json:"interface"`
	Duration  int    `json:"duration"`
	Packets   int    `json:"packets"`
	URI       string `json:"uri"`
}

func (e *VyOSTraceExecutor) Execute(ctx context.Context, msg agentcore.ActionCommand) (Output, error) {
	if ctx == nil {
		return Output{}, errors.New("execute action: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Output{}, fmt.Errorf("execute action: %w", err)
	}
	if msg.Target == "" {
		return Output{}, errors.New("execute action: target is empty")
	}
	if msg.Action != ActionTrace {
		return Output{}, fmt.Errorf("execute action: unsupported action %q", msg.Action)
	}
	if msg.RPCID == "" {
		return Output{}, errors.New("execute action: rpc id is empty")
	}
	if len(msg.Payload) == 0 || !json.Valid(msg.Payload) {
		return Output{}, fmt.Errorf("%w: payload must be valid json", ErrInvalidActionPayload)
	}

	var payload tracePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return Output{}, fmt.Errorf("%w: failed to unmarshal payload: %v", ErrInvalidActionPayload, err)
	}

	// Validate parameter bounds
	if payload.Duration < 0 || payload.Duration > maxDuration {
		return Output{}, fmt.Errorf("%w: duration %d must be between 0 and %d seconds", ErrInvalidActionPayload, payload.Duration, maxDuration)
	}
	if payload.Packets < 0 || payload.Packets > maxPackets {
		return Output{}, fmt.Errorf("%w: packets %d must be between 0 and %d", ErrInvalidActionPayload, payload.Packets, maxPackets)
	}

	// Validate interface
	interfaceName := payload.Interface
	if len(interfaceName) == 0 {
		return Output{}, fmt.Errorf("%w: interface is required", ErrInvalidActionPayload)
	}
	if strings.Contains(interfaceName, "..") || strings.Contains(interfaceName, "/") || strings.Contains(interfaceName, "\\") {
		return Output{}, fmt.Errorf("%w: invalid interface name %q", ErrInvalidActionPayload, interfaceName)
	}

	var interfaceValid bool
	if _, err := os.Stat("/sys/class/net/" + interfaceName); err == nil {
		interfaceValid = true
	} else {
		if vyosInterfaceRegex.MatchString(interfaceName) {
			interfaceValid = true
		}
	}
	if !interfaceValid {
		return Output{}, fmt.Errorf("%w: invalid interface name %q", ErrInvalidActionPayload, interfaceName)
	}

	// Validate URI
	if len(payload.URI) == 0 {
		return Output{}, fmt.Errorf("%w: uri is required", ErrInvalidActionPayload)
	}
	parsedURI, err := url.Parse(payload.URI)
	if err != nil || (parsedURI.Scheme != "http" && parsedURI.Scheme != "https") {
		return Output{}, fmt.Errorf("%w: invalid upload uri %q", ErrInvalidActionPayload, payload.URI)
	}

	// Configure temporary PCAP file path securely
	tempFile, err := os.CreateTemp("", "pcap-*.pcap")
	if err != nil {
		return Output{}, fmt.Errorf("create temporary pcap file: %w", err)
	}
	pcapPath := tempFile.Name()
	tempFile.Close() // Immediately close so tcpdump can write to it

	defer func() {
		_ = os.Remove(pcapPath)
	}()

	// Build tcpdump arguments
	args := []string{"-U", "-i", payload.Interface, "-w", pcapPath}
	if payload.Packets > 0 {
		args = append(args, "-c", strconv.Itoa(payload.Packets))
	}

	// Capture duration (defaults to 60 seconds if not specified)
	durationSec := payload.Duration
	if durationSec <= 0 {
		durationSec = 60
	}
	duration := time.Duration(durationSec) * time.Second

	// Sub-context for execution timeout
	captureCtx, captureCancel := context.WithTimeout(ctx, duration)
	defer captureCancel()

	cmd := e.runner.Command(captureCtx, "/usr/bin/tcpdump", args...)
	runErr := cmd.Run()

	// Verify command outcome
	if runErr != nil {
		if errors.Is(captureCtx.Err(), context.DeadlineExceeded) {
			// Success case: capture completed because the duration timeout expired
		} else if errors.Is(ctx.Err(), context.Canceled) {
			// Operation aborted by caller context cancellation
			return Output{}, fmt.Errorf("capture aborted: %w", ctx.Err())
		} else {
			// Real execution failure (e.g. permission error, interface doesn't exist)
			return Output{}, fmt.Errorf("packet capture failed: %w", runErr)
		}
	}

	// Upload resulting PCAP file
	if err := e.uploadFile(ctx, payload.URI, pcapPath); err != nil {
		return Output{}, fmt.Errorf("upload captured file: %w", err)
	}

	resultPayload, err := json.Marshal(map[string]any{
		"executor":         "vyos_trace",
		"action":           ActionTrace,
		"target":           msg.Target,
		"status":           "completed",
		"interface":        payload.Interface,
		"duration_seconds": durationSec,
		"pcap_file":        filepath.Base(pcapPath),
	})
	if err != nil {
		return Output{}, fmt.Errorf("build trace result payload: %w", err)
	}

	return Output{
		Payload: resultPayload,
		Message: "trace action completed",
	}, nil
}

func (e *VyOSTraceExecutor) uploadFile(ctx context.Context, uri, filePath string) error {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	contentType := writer.FormDataContentType()

	go func() {
		var err error
		defer func() {
			if err != nil {
				_ = pw.CloseWithError(err)
			} else {
				_ = pw.Close()
			}
		}()

		file, err := os.Open(filePath)
		if err != nil {
			return
		}
		defer file.Close()

		part, err := writer.CreateFormFile("file", filepath.Base(filePath))
		if err != nil {
			return
		}

		if _, err = io.Copy(part, file); err != nil {
			return
		}

		if err = writer.Close(); err != nil {
			return
		}
	}()

	req, err := http.NewRequestWithContext(ctx, "POST", uri, pr)
	if err != nil {
		return fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("send upload request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed status=%d response=%q", resp.StatusCode, string(respBody))
	}

	return nil
}
