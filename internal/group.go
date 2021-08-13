package core

import (
	"context"
	"reflect"
	"sync"

	"github.com/filecoin-project/go-statestore"
	"github.com/ipfs/go-datastore"
	eventbus "github.com/protocol/hack-the-bus"
	"golang.org/x/xerrors"
)

type InitFn func(<-chan struct{})
type CreateContextFn func(ctx context.Context, sm *StateMachine) interface{}

// StateGroup manages a group of state machines sharing the same logic
type StateGroup struct {
	SystemName      string
	Store           *statestore.StateStore
	EventBuses      eventbus.Collection
	planner         Planner
	initFn          InitFn
	stateType       reflect.Type
	createContextFn func(ctx context.Context, sm *StateMachine) interface{}
	closing         chan struct{}
	initNotifier    sync.Once

	lk  sync.Mutex
	sms map[datastore.Key]*StateMachine
}

// stateType: T - (MyStateStruct{})
func New(systemName string, ds datastore.Datastore, eventBusCollection eventbus.Collection, planner Planner, initFn InitFn, createContextFn CreateContextFn, stateType interface{}) *StateGroup {
	return &StateGroup{
		SystemName:      systemName,
		Store:           statestore.New(ds),
		EventBuses:      eventBusCollection,
		planner:         planner,
		initFn:          initFn,
		stateType:       reflect.TypeOf(stateType),
		createContextFn: createContextFn,
		closing:         make(chan struct{}),
		sms:             map[datastore.Key]*StateMachine{},
	}
}

func (s *StateGroup) init() {
	if s.initFn != nil {
		s.initFn(s.closing)
	}
}

func (s *StateGroup) Create(id interface{}, userState interface{}, producer eventbus.Producer, consumer eventbus.Consumer) (*StateMachine, error) {
	s.lk.Lock()
	defer s.lk.Unlock()

	sm, exist := s.sms[statestore.ToKey(id)]
	if exist {
		return nil, xerrors.Errorf("Begin: already tracking identifier `%v`", id)
	}

	exists, err := s.Store.Has(id)
	if err != nil {
		return nil, xerrors.Errorf("failed to check if state for %v exists: %w", id, err)
	}
	if exists {
		return nil, xerrors.Errorf("Begin: cannot initiate a state for identifier `%v` that already exists", id)
	}

	sm, err = s.create(id, userState, producer, consumer)
	if err != nil {
		return nil, xerrors.Errorf("loadOrCreate state: %w", err)
	}
	s.sms[statestore.ToKey(id)] = sm
	return sm, nil
}

// Send sends an event to machine identified by `id`.
// `evt` is going to be passed into StateHandler.Planner, in the events[].User param
//
// If a state machine with the specified id doesn't exits, it's created, and it's
// state is set to zero-value of stateType provided in group constructor
func (s *StateGroup) GetOrCreate(id interface{}, subscribedEvents []eventbus.EventType) (*StateMachine, error) {
	s.lk.Lock()
	defer s.lk.Unlock()

	sm, exist := s.sms[statestore.ToKey(id)]
	if !exist {
		userState := reflect.New(s.stateType).Interface()
		producer, consumer, err := s.GetOrCreateProducerConsumer(id, subscribedEvents)
		if err != nil {
			return nil, err
		}
		sm, err = s.create(id, userState, producer, consumer)
		if err != nil {
			return nil, xerrors.Errorf("loadOrCreate state: %w", err)
		}
		s.sms[statestore.ToKey(id)] = sm
	}

	return sm, nil
}

func (s *StateGroup) create(name interface{}, userState interface{}, producer eventbus.Producer, consumer eventbus.Consumer) (*StateMachine, error) {
	s.initNotifier.Do(s.init)
	exists, err := s.Store.Has(name)
	if err != nil {
		return nil, xerrors.Errorf("failed to check if state for %v exists: %w", name, err)
	}

	if !exists {
		if !reflect.TypeOf(userState).AssignableTo(reflect.PtrTo(s.stateType)) {
			return nil, xerrors.Errorf("initialized item with incorrect type %s", reflect.TypeOf(userState).Name())
		}

		err = s.Store.Begin(name, userState)
		if err != nil {
			return nil, xerrors.Errorf("saving initial state: %w", err)
		}
	}

	res := &StateMachine{
		planner:       s.planner,
		eventConsumer: consumer,
		eventProducer: producer,
		createContext: s.createContextFn,
		name:          name,
		st:            s.Store.Get(name),
		stateType:     s.stateType,

		stageDone: make(chan struct{}),
		closing:   make(chan struct{}),
		closed:    make(chan struct{}),
	}

	go res.run()
	return res, nil
}

func (s *StateGroup) GetOrCreateProducerConsumer(name interface{}, subscribedEvents []eventbus.EventType) (eventbus.Producer, eventbus.Consumer, error) {

	participant := eventbus.Participant{Type: s.SystemName, LocalID: name}

	b, err := s.EventBuses.GetBusByParticpant(participant)
	if err != nil {
		return nil, nil, xerrors.Errorf("failed to check if eventbus for %v exists: %w", name, err)
	}
	if b != nil {
		producer, err := b.LookupProducer(participant)
		if err != nil {
			return nil, nil, xerrors.Errorf("looking up producer: %w", err)
		}
		consumer, err := b.LookupConsumer(participant)
		if err != nil {
			return nil, nil, xerrors.Errorf("looking up consumer: %w", err)
		}
		return producer, consumer, nil
	}

	b, err = s.EventBuses.CreateNewBus([]eventbus.FailureCondition{})
	if err != nil {
		return nil, nil, xerrors.Errorf("creating event bus: %w", err)
	}
	producer, err := b.CreateProducer(participant)
	if err != nil {
		return nil, nil, xerrors.Errorf("creating producer: %w", err)
	}
	consumer, err := b.CreateConsumer(participant)
	if err != nil {
		return nil, nil, xerrors.Errorf("creating producer: %w", err)
	}
	return producer, consumer, nil
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

	close(s.closing)
	return nil
}
