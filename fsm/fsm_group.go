package fsm

import (
	"context"

	"github.com/filecoin-project/go-statemachine"
	"github.com/ipfs/go-datastore"
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
// world: any external interface this state needs to talk to
// stateType: the concrete type of state struct that will be managed
// stateKeyField: the field of the state struct that will serve as the
// key for the current state
// events: list of events that can be dispatched to the statemachine
// stateHandlers: callbacks that are called upon entering a particular state
// after processing events
func New(ds datastore.Datastore, world World, stateType StateType,
	stateField StateKeyField, events Events, stateHandlers StateHandlers) (Group, error) {
	handler, err := NewFSMHandler(world, stateType, stateField, events, stateHandlers)
	if err != nil {
		return nil, err
	}
	d := handler.(fsmHandler)
	return &stateGroup{StateGroup: statemachine.New(ds, handler, stateType), d: d}, nil
}

// Send sends the given event name and parameters to the state specified by id
// it will error if there are underlying state store errors or if the parameters
// do not match what is expected for the event name
func (s *stateGroup) Send(id interface{}, name EventName, args ...interface{}) (err error) {
	evt, err := s.d.event(context.TODO(), name, nil, args...)
	if err != nil {
		return err
	}
	return s.StateGroup.Send(id, evt)
}

// SendSync sends the given event name and parameters to the state specified by id
// it will error if there are underlying state store errors or if the parameters
// do not match what is expected for the event name
func (s *stateGroup) SendSync(ctx context.Context, id interface{}, name EventName, args ...interface{}) (err error) {
	returnChannel := make(chan error, 1)
	evt, err := s.d.event(ctx, name, returnChannel, args...)
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
		return err
	}
}
