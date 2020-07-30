package fsmtestutil

import (
	"context"
	"testing"

	"github.com/filecoin-project/go-statemachine"
	"github.com/filecoin-project/go-statemachine/fsm"
	"github.com/stretchr/testify/require"
)

// TestContext is a context you can wired up directly to an event machine so that you can test
// state entry functions and how they affect state
type TestContext struct {
	ctx              context.Context
	eventProcessor   fsm.EventProcessor
	dispatchedEvents []statemachine.Event
}

// NewTestContext returns a new test context for the given event machien
func NewTestContext(ctx context.Context, eventProcessor fsm.EventProcessor) *TestContext {
	return &TestContext{ctx, eventProcessor, nil}
}

// Context returns the golang context for this context
func (tc *TestContext) Context() context.Context { return tc.ctx }

// Trigger initiates a state transition with the named event.
//
// The call takes a variable number of arguments that will be passed to the
// callback, if defined.
//
// It will return nil if the event is one of these errors:
//
// - event X does not exist
//
// - arguments don't match expected transition
func (tc *TestContext) Trigger(event fsm.EventName, args ...interface{}) error {
	evt, err := tc.eventProcessor.Generate(tc.ctx, event, nil, args...)
	if err != nil {
		return err
	}
	tc.dispatchedEvents = append(tc.dispatchedEvents, statemachine.Event{User: evt})
	return nil
}

// ReplayEvents will use the eventProcessor to attempt to perform the dispatched
// transitions on the given state object. it will fail the test if any of them fail
func (tc *TestContext) ReplayEvents(t *testing.T, user interface{}) {
	for _, evt := range tc.dispatchedEvents {
		_, err := tc.eventProcessor.Apply(evt, user)
		noError := err == nil
		_, skipHandler := err.(fsm.ErrSkipHandler)
		require.True(t, noError || skipHandler)
	}
}

var _ fsm.Context = &TestContext{}
