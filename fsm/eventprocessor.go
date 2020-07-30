package fsm

import (
	"context"
	"reflect"

	"github.com/filecoin-project/go-statemachine"
	"golang.org/x/xerrors"
)

// EventProcessor creates and applies events for go-statemachine based on the given event list
type EventProcessor interface {
	// Event generates an event that can be dispatched to go-statemachine from the given event name and context args
	Generate(ctx context.Context, event EventName, returnChannel chan error, args ...interface{}) (interface{}, error)
	// Apply applies the given event from go-statemachine to the given state, based on transition rules
	Apply(evt statemachine.Event, user interface{}) (EventName, error)
	// ClearEvents clears out events that are synchronous with the given error message
	ClearEvents(evts []statemachine.Event, err error)
}

type eventProcessor struct {
	stateType     reflect.Type
	stateKeyField StateKeyField
	callbacks     map[EventName]callback
	transitions   map[eKey]StateKey
}

// eKey is a struct key used for storing the transition map.
type eKey struct {
	// event is the name of the event that the keys refers to.
	event EventName

	// src is the source from where the event can transition.
	src interface{}
}

// callback stores a transition function and its argument types
type callback struct {
	argumentTypes []reflect.Type
	action        ActionFunc
}

// fsmEvent is the internal event type
type fsmEvent struct {
	name          EventName
	args          []interface{}
	ctx           context.Context
	returnChannel chan error
}

type nilEvent struct{}

// ErrSkipHandler is a sentinel type that indicates not an error but that we should skip any state handlers present
type ErrSkipHandler struct{}

func (e ErrSkipHandler) Error() string {
	return "record event does not get handler"
}

// NewEventProcessor returns a new event machine for the given state and event list
func NewEventProcessor(state StateType, stateKeyField StateKeyField, events []EventBuilder) (EventProcessor, error) {
	err := VerifyEventParameters(state, stateKeyField, events)
	if err != nil {
		return nil, err
	}
	stateType := reflect.TypeOf(state)

	em := eventProcessor{
		stateType:     stateType,
		stateKeyField: stateKeyField,
		callbacks:     make(map[EventName]callback),
		transitions:   make(map[eKey]StateKey),
	}

	// Build transition map and store sets of all events and states.
	for _, evtIface := range events {
		evt := evtIface.(eventBuilder)

		name := evt.name

		var argumentTypes []reflect.Type
		if evt.action != nil {
			atType := reflect.TypeOf(evt.action)
			argumentTypes = make([]reflect.Type, atType.NumIn()-1)
			for i := range argumentTypes {
				argumentTypes[i] = atType.In(i + 1)
			}
		}
		em.callbacks[name] = callback{
			argumentTypes: argumentTypes,
			action:        evt.action,
		}
		for src, dst := range evt.transitionsSoFar {
			em.transitions[eKey{name, src}] = dst
		}
	}
	return em, nil
}

// Event generates an event that can be dispatched to go-statemachine from the given event name and context args
func (em eventProcessor) Generate(ctx context.Context, event EventName, returnChannel chan error, args ...interface{}) (interface{}, error) {
	if _, ok := event.(nilEvent); ok {
		return fsmEvent{event, nil, ctx, returnChannel}, nil
	}
	cb, ok := em.callbacks[event]
	if !ok {
		return fsmEvent{}, xerrors.Errorf("Unknown event `%+v`", event)
	}
	if len(args) != len(cb.argumentTypes) {
		return fsmEvent{}, xerrors.Errorf("Wrong number of arguments for event `%+v`", event)
	}
	for i, arg := range args {
		if !reflect.TypeOf(arg).AssignableTo(cb.argumentTypes[i]) {
			return fsmEvent{}, xerrors.Errorf("Incorrect argument type at index `%d` for event `%+v`", i, event)
		}
	}
	return fsmEvent{event, args, ctx, returnChannel}, nil
}

func (em eventProcessor) Apply(evt statemachine.Event, user interface{}) (EventName, error) {
	e, ok := evt.User.(fsmEvent)
	if !ok {
		return nil, xerrors.New("Not an fsm event")
	}
	if _, ok := e.name.(nilEvent); ok {
		return e.name, completeEvent(e, nil)
	}
	userValue := reflect.ValueOf(user)
	currentState := userValue.Elem().FieldByName(string(em.stateKeyField)).Interface()

	destination, ok := em.transitions[eKey{e.name, currentState}]
	// check for fallback transition for any source state
	if !ok {
		destination, ok = em.transitions[eKey{e.name, nil}]
	}
	if !ok {
		return nil, completeEvent(e, xerrors.Errorf("Invalid transition in queue, state `%+v`, event `%+v`", currentState, e.name))
	}
	cb := em.callbacks[e.name]
	err := applyAction(userValue, e, cb)
	if err != nil {
		return nil, completeEvent(e, err)
	}
	if _, ok := destination.(recordEvent); ok {
		return e.name, completeEvent(e, ErrSkipHandler{})
	}
	if destination != nil {
		userValue.Elem().FieldByName(string(em.stateKeyField)).Set(reflect.ValueOf(destination))
	}

	return e.name, completeEvent(e, nil)
}

func (em eventProcessor) ClearEvents(evts []statemachine.Event, err error) {
	for _, evt := range evts {
		fsmEvt, ok := evt.User.(fsmEvent)
		if ok {
			_ = completeEvent(fsmEvt, err)
		}
	}
}

// Apply applies the given event from go-statemachine to the given state, based on transition rules
func applyAction(userValue reflect.Value, e fsmEvent, cb callback) error {
	if cb.action == nil {
		return nil
	}
	values := make([]reflect.Value, 0, len(e.args)+1)
	values = append(values, userValue)
	for _, arg := range e.args {
		values = append(values, reflect.ValueOf(arg))
	}
	res := reflect.ValueOf(cb.action).Call(values)

	if res[0].Interface() != nil {
		return xerrors.Errorf("Error applying event transition `%+v`: %w", e.name, res[0].Interface().(error))
	}
	return nil
}

func completeEvent(event fsmEvent, err error) error {
	if event.returnChannel != nil {
		select {
		case <-event.ctx.Done():
		case event.returnChannel <- err:
		}
	}
	return err
}
