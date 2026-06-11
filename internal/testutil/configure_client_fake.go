package testutil

import (
	"context"
	"sync"

	"github.com/routerarchitects/nats-agent-core/agentcore"
)

// FakeConfigureClient is a controllable configure client test double.
type FakeConfigureClient struct {
	StatusResultRecorder

	Desired         *agentcore.StoredDesiredConfig
	DesiredByTarget map[string]*agentcore.StoredDesiredConfig
	LoadErr         error
	Events          *EventRecorder

	mu        sync.Mutex
	loadCalls int
}

func (f *FakeConfigureClient) LoadDesiredConfig(ctx context.Context, target string) (*agentcore.StoredDesiredConfig, error) {
	if f.Events != nil {
		f.Events.Record("load_desired")
	}

	f.mu.Lock()
	f.loadCalls++
	err := f.LoadErr
	desired := f.Desired
	if f.DesiredByTarget != nil {
		desired = f.DesiredByTarget[target]
	}
	f.mu.Unlock()

	if err != nil {
		return nil, err
	}
	if desired == nil {
		return nil, nil
	}
	out := cloneStoredDesiredConfig(*desired)
	return &out, nil
}

func (f *FakeConfigureClient) LoadCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loadCalls
}

func (f *FakeConfigureClient) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.loadCalls = 0
	f.StatusResultRecorder.Reset()
}

func cloneStoredDesiredConfig(in agentcore.StoredDesiredConfig) agentcore.StoredDesiredConfig {
	in.Record.Payload = cloneRawMessage(in.Record.Payload)
	return in
}
