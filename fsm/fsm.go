package fsm

import (
	"context"
	"reflect"

	"github.com/filecoin-project/go-statemachine"
	logging "github.com/ipfs/go-log"
)

var log = logging.Logger("fsm")

type fsmHandler struct {
	stateType       reflect.Type
	stateKeyField   StateKeyField
	notifier        Notifier
	notifications   chan notification
	eventProcessor  EventProcessor
	stateEntryFuncs StateEntryFuncs
	environment     Environment
	finalityStates  map[StateKey]struct{}
}

const NotificationQueueSize = 128

type notification struct {
	eventName EventName
	state     StateType
}

// NewFSMHandler defines an StateHandler for go-statemachine that implements
// a traditional Finite State Machine model -- transitions, start states,
// end states, and callbacks
func NewFSMHandler(parameters Parameters) (statemachine.StateHandler, error) {
	err := VerifyStateParameters(parameters)
	if err != nil {
		return nil, err
	}
	stateType := reflect.TypeOf(parameters.StateType)

	eventProcessor, err := NewEventProcessor(parameters.StateType, parameters.StateKeyField, parameters.Events)
	if err != nil {
		return nil, err
	}
	d := fsmHandler{
		environment:     parameters.Environment,
		stateType:       stateType,
		stateKeyField:   parameters.StateKeyField,
		eventProcessor:  eventProcessor,
		stateEntryFuncs: make(StateEntryFuncs),
		notifier:        parameters.Notifier,
		finalityStates:  make(map[StateKey]struct{}),
	}

	// type check state handlers
	for state, stateEntryFunc := range parameters.StateEntryFuncs {
		d.stateEntryFuncs[state] = stateEntryFunc
	}

	for _, finalityState := range parameters.FinalityStates {
		d.finalityStates[finalityState] = struct{}{}
	}

	if d.notifier != nil {
		d.notifications = make(chan notification)
	}

	return d, nil
}

// Plan executes events according to finite state machine logic
// It checks to see if the events can applied based on the current state,
// then applies the transition, updating the keyed state in the process
// It only applies one event per planning, to preserve predictable behavior
// for the statemachine -- given a set of events, received in a given order
// the exact same updates will occur, and the exact same state handlers will get
// called
// At the end it executes the specified handler for the final state,
// if specified
func (d fsmHandler) Plan(events []statemachine.Event, user interface{}) (interface{}, uint64, error) {
	userValue := reflect.ValueOf(user)
	if d.reachedFinalityState(userValue.Elem().Interface()) {
		d.eventProcessor.ClearEvents(events, statemachine.ErrTerminated)
		return nil, uint64(len(events)), statemachine.ErrTerminated
	}
	eventName, err := d.eventProcessor.Apply(events[0], user)
	if _, ok := eventName.(nilEvent); ok {
		return nil, 1, nil
	}
	_, skipHandler := err.(ErrSkipHandler)
	if err != nil && !skipHandler {
		log.Errorf("Executing event planner failed for %s: %+v", d.stateType.Name(), err)
		return nil, 1, nil
	}
	currentState := userValue.Elem().FieldByName(string(d.stateKeyField)).Interface()
	if d.notifier != nil {
		d.notifications <- notification{eventName, userValue.Elem().Interface()}
	}
	_, final := d.finalityStates[currentState]
	if final {
		d.eventProcessor.ClearEvents(events[1:], statemachine.ErrTerminated)
		return nil, uint64(len(events)), statemachine.ErrTerminated
	}
	if skipHandler {
		return d.handler(nil), 1, nil
	}
	return d.handler(d.stateEntryFuncs[currentState]), 1, nil
}

func (d fsmHandler) reachedFinalityState(user interface{}) bool {
	userValue := reflect.ValueOf(user)
	currentState := userValue.FieldByName(string(d.stateKeyField)).Interface()
	_, final := d.finalityStates[currentState]
	return final
}

// Init will start up a goroutine which processes the notification queue
// in order
func (d fsmHandler) Init(closing <-chan struct{}) {
	if d.notifier != nil {
		queue := make([]notification, 0, NotificationQueueSize)
		toProcess := make(chan notification)
		go func() {
			for {
				select {
				case n := <-toProcess:
					d.notifier(n.eventName, n.state)
				case <-closing:
					return
				}
			}
		}()
		go func() {
			outgoing := func() chan<- notification {
				if len(queue) == 0 {
					return nil
				}
				return toProcess
			}
			nextNotification := func() notification {
				if len(queue) == 0 {
					return notification{}
				}
				return queue[0]
			}
			for {
				select {
				case n := <-d.notifications:
					queue = append(queue, n)
				case outgoing() <- nextNotification():
					queue = queue[1:]
				case <-closing:
					return
				}
			}
		}()
	}
}

// handler makes a state next step function from the given callback
func (d fsmHandler) handler(cb interface{}) interface{} {
	if cb == nil {
		return nil
	}
	handlerType := reflect.FuncOf([]reflect.Type{reflect.TypeOf(statemachine.Context{}), d.stateType}, []reflect.Type{reflect.TypeOf(new(error)).Elem()}, false)
	return reflect.MakeFunc(handlerType, func(args []reflect.Value) (results []reflect.Value) {
		ctx := args[0].Interface().(statemachine.Context)
		state := args[1].Interface()
		dContext := fsmContext{state, ctx, d}
		return reflect.ValueOf(cb).Call([]reflect.Value{reflect.ValueOf(dContext), reflect.ValueOf(d.environment), args[1]})
	}).Interface()
}

type fsmContext struct {
	state interface{}
	ctx   statemachine.Context
	d     fsmHandler
}

func (dc fsmContext) Context() context.Context {
	return dc.ctx.Context()
}

func (dc fsmContext) Trigger(event EventName, args ...interface{}) error {
	evt, err := dc.d.eventProcessor.Generate(dc.ctx.Context(), event, nil, args...)
	if err != nil {
		return err
	}
	return dc.ctx.Send(evt)
}

var _ Context = fsmContext{}
