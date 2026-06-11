package testutil

import (
	"context"
	"sync"

	"github.com/routerarchitects/olg-server-vyos-client-natagent/internal/renderer"
)

// FakeApplyEngine is a controllable configure ApplyEngine test double.
type FakeApplyEngine struct {
	Err      error
	Validate func(renderer.Output) error
	Events   *EventRecorder

	mu     sync.Mutex
	calls  int
	inputs []renderer.Output
}

func (f *FakeApplyEngine) Apply(ctx context.Context, rendered renderer.Output) error {
	if f.Events != nil {
		f.Events.Record("apply")
	}

	f.mu.Lock()
	f.calls++
	f.inputs = append(f.inputs, rendered)
	validate := f.Validate
	err := f.Err
	f.mu.Unlock()

	if validate != nil {
		if err := validate(rendered); err != nil {
			return err
		}
	}
	return err
}

func (f *FakeApplyEngine) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *FakeApplyEngine) Inputs() []renderer.Output {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]renderer.Output(nil), f.inputs...)
}

func (f *FakeApplyEngine) LastInput() (renderer.Output, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.inputs) == 0 {
		return renderer.Output{}, false
	}
	return f.inputs[len(f.inputs)-1], true
}

func (f *FakeApplyEngine) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = 0
	f.inputs = nil
}
