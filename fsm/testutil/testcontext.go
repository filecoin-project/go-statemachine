package fsmtestutil

import (
	"context"
	"testing"

	"github.com/filecoin-project/go-statemachine"
	"github.com/filecoin-project/go-statemachine/fsm"
	"github.com/stretchr/testify/require"
)

// TestContext is a context you can wired up directly to an event machine so that you can test
// statehandlers and how they affect state
type TestContext struct {
	ctx              context.Context
	eventmachine     fsm.EventMachine
	dispatchedEvents []statemachine.Event
}

// NewTestContext returns a new test context for the given event machien
func NewTestContext(ctx context.Context, eventMachine fsm.EventMachine) *TestContext {
	return &TestContext{ctx, eventMachine, nil}
}

// Context returns the golang context for this context
func (tc *TestContext) Context() context.Context { return tc.ctx }

// Event initiates a state transition with the named event.
//
// The call takes a variable number of arguments that will be passed to the
// callback, if defined.
//
// It will return nil if the event is one of these errors:
//
// - event X does not exist
//
// - arguments don't match expected transition
func (tc *TestContext) Event(event fsm.EventName, args ...interface{}) error {
	evt, err := tc.eventmachine.Event(tc.ctx, event, nil, args...)
	if err != nil {
		return err
	}
	tc.dispatchedEvents = append(tc.dispatchedEvents, statemachine.Event{User: evt})
	return nil
}

// ReplayEvents will use the eventmachine to attempt to perform the dispatched
// transitions on the given state object. it will fail the test if any of them fail
func (tc *TestContext) ReplayEvents(t *testing.T, user interface{}) {
	for _, evt := range tc.dispatchedEvents {
		_, err := tc.eventmachine.Apply(evt, user)
		require.NoError(t, err)
	}
}
