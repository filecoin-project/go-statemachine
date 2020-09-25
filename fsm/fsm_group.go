package fsm

import (
	"context"

	"github.com/filecoin-project/go-statemachine"
	"github.com/ipfs/go-datastore"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"
)

// StateGroup manages a group of states with finite state machine logic
type stateGroup struct {
	*statemachine.StateGroup
	d fsmHandler
}

// New generates a new state group that operates like a finite state machine,
// based on the given parameters
// ds: data store where state comes from
// parameters: finite state machine parameters
func New(ds datastore.Datastore, parameters Parameters) (Group, error) {
	handler, err := NewFSMHandler(parameters)
	if err != nil {
		return nil, err
	}
	d := handler.(fsmHandler)
	return &stateGroup{StateGroup: statemachine.New(ds, handler, parameters.StateType), d: d}, nil
}

// Send sends the given event name and parameters to the state specified by id
// it will error if there are underlying state store errors or if the parameters
// do not match what is expected for the event name
func (s *stateGroup) Send(id interface{}, name EventName, args ...interface{}) (err error) {
	evt, err := s.d.eventProcessor.Generate(context.TODO(), name, nil, args...)
	if err != nil {
		return err
	}
	return s.StateGroup.Send(id, evt)
}

// IsTerminated returns true if a StateType is in a FinalityState
func (s *stateGroup) IsTerminated(out StateType) bool {
	return s.d.reachedFinalityState(out)
}

// Get gets state for a single state machine
func (s *stateGroup) Get(id interface{}) StoredState {
	return s.StateGroup.Get(id)
}

// GetSync will make sure all events present at the time of the call are processed before
// returning a value, which is read into out
func (s *stateGroup) GetSync(ctx context.Context, id interface{}, out cbg.CBORUnmarshaler) error {
	err := s.SendSync(ctx, id, nilEvent{})
	if err != nil && err != statemachine.ErrTerminated {
		return err
	}
	return s.Get(id).Get(out)
}

// SendSync sends the given event name and parameters to the state specified by id
// it will error if there are underlying state store errors or if the parameters
// do not match what is expected for the event name
func (s *stateGroup) SendSync(ctx context.Context, id interface{}, name EventName, args ...interface{}) (err error) {
	returnChannel := make(chan error, 1)
	evt, err := s.d.eventProcessor.Generate(ctx, name, returnChannel, args...)
	if err != nil {
		return err
	}

	err = s.StateGroup.Send(id, evt)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return xerrors.New("Context cancelled")
	case err := <-returnChannel:
		_, justRecord := err.(ErrSkipHandler)
		if justRecord {
			return nil
		}
		return err
	}
}
