package statemachine

import (
	"context"

	core "github.com/filecoin-project/go-statemachine/internal"
	"github.com/filecoin-project/go-statestore"
	"github.com/ipfs/go-datastore"
	logging "github.com/ipfs/go-log"
	eventbus "github.com/protocol/hack-the-bus"
	"golang.org/x/xerrors"
)

type StateHandler interface {
	// returns
	Plan(events []Event, user interface{}) (interface{}, uint64, error)
}

type StateHandlerWithInit interface {
	StateHandler
	Init(<-chan struct{})
}

var log = logging.Logger("evtsm")

var ErrTerminated = xerrors.New("normal shutdown of state machine")

type Event struct {
	User interface{}
}

type internalEvent struct{ systemName string }

// Planner processes in queue events
// It returns:
// 1. a handler of type -- func(ctx Context, st <T>) (func(*<T>), error), where <T> is the typeOf(User) param
// 2. the number of events processed
// 3. an error if occured
type Planner func(events []Event, user interface{}) (interface{}, uint64, error)

// StateGroup manages a group of state machines sharing the same logic
type StateGroup struct {
	group *core.StateGroup
}

// stateType: T - (MyStateStruct{})
func New(systemName string, ds datastore.Datastore, eventBusCollection eventbus.Collection, hnd StateHandler, stateType interface{}) *StateGroup {
	var initFn core.InitFn
	if initter, ok := hnd.(StateHandlerWithInit); ok {
		initFn = initter.Init
	}
	planner := func(events []eventbus.Event, user interface{}) (interface{}, uint64, error) {
		internalEvents := make([]Event, 0, len(events))
		for _, event := range events {
			internalEvents = append(internalEvents, event.Data().(Event))
		}
		return hnd.Plan(internalEvents, user)
	}
	createContextFn := func(ctx context.Context, sm *core.StateMachine) interface{} {
		return Context{
			ctx:        ctx,
			sm:         sm,
			systemName: systemName,
		}
	}
	group := core.New(systemName, ds, eventBusCollection, planner, initFn, createContextFn, stateType)
	return &StateGroup{
		group: group,
	}
}

// Begin initiates tracking with a specific value for a given identifier
func (s *StateGroup) Begin(id interface{}, userState interface{}) error {
	producer, consumer, err := s.group.GetOrCreateProducerConsumer(id, []eventbus.EventType{internalEvent{s.group.SystemName}})
	if err != nil {
		return err
	}
	_, err = s.group.Create(id, userState, producer, consumer)
	return err
}

// Send sends an event to machine identified by `id`.
// `evt` is going to be passed into StateHandler.Planner, in the events[].User param
//
// If a state machine with the specified id doesn't exits, it's created, and it's
// state is set to zero-value of stateType provided in group constructor
func (s *StateGroup) Send(id interface{}, evt interface{}) (err error) {
	sm, err := s.group.GetOrCreate(id, []eventbus.EventType{internalEvent{s.group.SystemName}})
	return sm.PublishEvent(internalEvent{s.group.SystemName}, Event{User: evt})
}

// Stop stops all state machines in this group
func (s *StateGroup) Stop(ctx context.Context) error {
	return s.group.Stop(ctx)
}

// List outputs states of all state machines in this group
// out: *[]StateT
func (s *StateGroup) List(out interface{}) error {
	return s.group.Store.List(out)
}

// Get gets state for a single state machine
func (s *StateGroup) Get(id interface{}) *statestore.StoredState {
	return s.group.Store.Get(id)
}

// Has indicates whether there is data for the given state machine
func (s *StateGroup) Has(id interface{}) (bool, error) {
	return s.group.Store.Has(id)
}
