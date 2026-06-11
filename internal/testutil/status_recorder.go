package testutil

import (
	"context"
	"sync"

	"github.com/routerarchitects/nats-agent-core/agentcore"
)

// StatusRecorder captures status envelopes in publish order.
type StatusRecorder struct {
	Err        error
	ErrByStage map[string]error
	Events     *EventRecorder

	mu       sync.Mutex
	statuses []agentcore.StatusEnvelope
}

func (r *StatusRecorder) PublishStatus(ctx context.Context, msg agentcore.StatusEnvelope) error {
	r.RecordStatus(msg)

	r.mu.Lock()
	err := r.Err
	if r.ErrByStage != nil && r.ErrByStage[msg.Stage] != nil {
		err = r.ErrByStage[msg.Stage]
	}
	r.mu.Unlock()
	return err
}

func (r *StatusRecorder) RecordStatus(msg agentcore.StatusEnvelope) {
	if r.Events != nil {
		r.Events.Record("publish_status")
		if msg.Status == "success" {
			r.Events.Record("publish_success_status")
		}
		if msg.Status == "failure" {
			r.Events.Record("publish_failure_status")
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses = append(r.statuses, msg)
}

func (r *StatusRecorder) Statuses() []agentcore.StatusEnvelope {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]agentcore.StatusEnvelope(nil), r.statuses...)
}

func (r *StatusRecorder) StatusStages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	stages := make([]string, 0, len(r.statuses))
	for _, status := range r.statuses {
		stages = append(stages, status.Stage)
	}
	return stages
}

func (r *StatusRecorder) LastStatus() (agentcore.StatusEnvelope, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.statuses) == 0 {
		return agentcore.StatusEnvelope{}, false
	}
	return r.statuses[len(r.statuses)-1], true
}

func (r *StatusRecorder) FindStatus(status, stage string) (agentcore.StatusEnvelope, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, msg := range r.statuses {
		if msg.Status == status && msg.Stage == stage {
			return msg, true
		}
	}
	return agentcore.StatusEnvelope{}, false
}

func (r *StatusRecorder) ContainsStatus(status, stage string) bool {
	_, ok := r.FindStatus(status, stage)
	return ok
}

func (r *StatusRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses = nil
}

// ResultRecorder captures result envelopes in publish order.
type ResultRecorder struct {
	Err            error
	ErrByResult    map[string]error
	ErrByErrorCode map[string]error
	Events         *EventRecorder

	mu      sync.Mutex
	results []agentcore.ResultEnvelope
}

func (r *ResultRecorder) PublishResult(ctx context.Context, msg agentcore.ResultEnvelope) error {
	r.RecordResult(msg)

	r.mu.Lock()
	err := r.Err
	if r.ErrByResult != nil && r.ErrByResult[msg.Result] != nil {
		err = r.ErrByResult[msg.Result]
	}
	if r.ErrByErrorCode != nil && r.ErrByErrorCode[msg.ErrorCode] != nil {
		err = r.ErrByErrorCode[msg.ErrorCode]
	}
	r.mu.Unlock()
	return err
}

func (r *ResultRecorder) RecordResult(msg agentcore.ResultEnvelope) {
	if r.Events != nil {
		switch msg.Result {
		case "success":
			r.Events.Record("publish_success")
		case "failure":
			r.Events.Record("publish_failure")
		default:
			r.Events.Record("publish_result")
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, msg)
}

func (r *ResultRecorder) Results() []agentcore.ResultEnvelope {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]agentcore.ResultEnvelope(nil), r.results...)
}

func (r *ResultRecorder) LastResult() (agentcore.ResultEnvelope, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.results) == 0 {
		return agentcore.ResultEnvelope{}, false
	}
	return r.results[len(r.results)-1], true
}

func (r *ResultRecorder) FindResult(result, commandType string) (agentcore.ResultEnvelope, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, msg := range r.results {
		if msg.Result == result && msg.CommandType == commandType {
			return msg, true
		}
	}
	return agentcore.ResultEnvelope{}, false
}

func (r *ResultRecorder) ContainsResult(result, commandType string) bool {
	_, ok := r.FindResult(result, commandType)
	return ok
}

func (r *ResultRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = nil
}

// StatusResultRecorder is a combined status/result publisher for tests.
type StatusResultRecorder struct {
	StatusRecorder
	ResultRecorder
}

func (r *StatusResultRecorder) Reset() {
	r.StatusRecorder.Reset()
	r.ResultRecorder.Reset()
}
