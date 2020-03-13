package statemachine

import (
	"context"
	"reflect"
	"sync"

	"github.com/filecoin-project/go-statestore"
	"github.com/ipfs/go-datastore"
	"golang.org/x/xerrors"
)

// StateHandler is any struct that implementings a plan function
type StateHandler interface {
	// returns
	Plan(events []Event, user interface{}) (interface{}, uint64, error)
}

var defaultFinisher Finisher = func(id interface{}, err error) {
	if err != nil {
		log.Errorf("FSM Errored During Operation: %w", err)
	}
}

// StateGroup manages a group of state machines sharing the same logic
type StateGroup struct {
	sts       *statestore.StateStore
	hnd       StateHandler
	stateType reflect.Type
	finisher  Finisher

	lk  sync.RWMutex
	sms map[datastore.Key]*StateMachine
}

// stateType: T - (MyStateStruct{})
func New(ds datastore.Datastore, hnd StateHandler, stateType interface{}) *StateGroup {
	return &StateGroup{
		sts:       statestore.New(ds),
		hnd:       hnd,
		finisher:  defaultFinisher,
		stateType: reflect.TypeOf(stateType),

		sms: map[datastore.Key]*StateMachine{},
	}
}

// Begin initiates tracking with a specific value for a given identifier
func (s *StateGroup) Begin(id interface{}, userState interface{}) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	sm, exist := s.sms[statestore.ToKey(id)]
	if exist {
		return xerrors.Errorf("Begin: already tracking identifier `%v`", id)
	}

	exists, err := s.sts.Has(id)
	if err != nil {
		return xerrors.Errorf("failed to check if state for %v exists: %w", id, err)
	}
	if exists {
		return xerrors.Errorf("Begin: cannot initiate a state for identifier `%v` that already exists", id)
	}

	sm, err = s.loadOrCreate(id, userState)
	if err != nil {
		return xerrors.Errorf("loadOrCreate state: %w", err)
	}
	s.sms[statestore.ToKey(id)] = sm
	return nil
}

// Send sends an event to machine identified by `id`.
// `evt` is going to be passed into StateHandler.Planner, in the events[].User param
//
// If a state machine with the specified id doesn't exits, it's created, and it's
// state is set to zero-value of stateType provided in group constructor
func (s *StateGroup) Send(id interface{}, evt interface{}) (err error) {
	s.lk.Lock()
	defer s.lk.Unlock()

	sm, exist := s.sms[statestore.ToKey(id)]
	if !exist {
		userState := reflect.New(s.stateType).Interface()
		sm, err = s.loadOrCreate(id, userState)
		if err != nil {
			return xerrors.Errorf("loadOrCreate state: %w", err)
		}
		s.sms[statestore.ToKey(id)] = sm
	}

	return sm.send(Event{User: evt})
}

func (s *StateGroup) loadOrCreate(name interface{}, userState interface{}) (*StateMachine, error) {
	exists, err := s.sts.Has(name)
	if err != nil {
		return nil, xerrors.Errorf("failed to check if state for %v exists: %w", name, err)
	}

	if !exists {
		if !reflect.TypeOf(userState).AssignableTo(reflect.PtrTo(s.stateType)) {
			return nil, xerrors.Errorf("initialized item with incorrect type %s", reflect.TypeOf(userState).Name())
		}

		err = s.sts.Begin(name, userState)
		if err != nil {
			return nil, xerrors.Errorf("saving initial state: %w", err)
		}
	}

	res := &StateMachine{
		planner:   s.hnd.Plan,
		eventsIn:  make(chan Event),
		finisher:  s.finish,
		name:      name,
		st:        s.sts.Get(name),
		stateType: s.stateType,

		stageDone: make(chan struct{}),
		closing:   make(chan struct{}),
		closed:    make(chan struct{}),
	}

	go res.run()

	return res, nil
}

func (s *StateGroup) finish(name interface{}, err error) {
	s.lk.Lock()
	delete(s.sms, statestore.ToKey(name))
	s.lk.Unlock()
	s.finisher(name, err)
}

// Stop stops all state machines in this group
func (s *StateGroup) Stop(ctx context.Context) error {
	s.lk.Lock()
	defer s.lk.Unlock()

	for _, sm := range s.sms {
		if err := sm.stop(ctx); err != nil {
			return err
		}
	}

	return nil
}

// IsRunning returns true if there is a running statemachine
// for the given identifier
func (s *StateGroup) IsRunning(id interface{}) bool {
	s.lk.RLock()
	defer s.lk.RUnlock()
	_, running := s.sms[statestore.ToKey(id)]
	return running
}

func (s *StateGroup) SetOnShutdownHandler(finisher Finisher) {
	s.finisher = finisher
}

// List outputs states of all state machines in this group
// out: *[]StateT
func (s *StateGroup) List(out interface{}) error {
	return s.sts.List(out)
}

// Get gets state for a single state machine
func (s *StateGroup) Get(id interface{}) *statestore.StoredState {
	return s.sts.Get(id)
}
