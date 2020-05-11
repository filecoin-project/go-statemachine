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

// NewEventProcessor returns a new event machine for the given state and event list
func NewEventProcessor(state StateType, stateKeyField StateKeyField, events []EventBuilder) (EventProcessor, error) {
	stateType := reflect.TypeOf(state)
	stateFieldType, ok := stateType.FieldByName(string(stateKeyField))
	if !ok {
		return nil, xerrors.Errorf("state type has no field `%s`", stateKeyField)
	}
	if !stateFieldType.Type.Comparable() {
		return nil, xerrors.Errorf("state field `%s` is not comparable", stateKeyField)
	}

	em := eventProcessor{
		stateType:     stateType,
		stateKeyField: stateKeyField,
		callbacks:     make(map[EventName]callback),
		transitions:   make(map[eKey]StateKey),
	}

	// Build transition map and store sets of all events and states.
	for _, evtIface := range events {
		evt, ok := evtIface.(eventBuilder)
		if !ok {
			errEvt := evtIface.(errBuilder)
			return nil, errEvt.err
		}

		name := evt.name

		_, exists := em.callbacks[name]
		if exists {
			return nil, xerrors.Errorf("Duplicate event name `%+v`", name)
		}

		argumentTypes, err := inspectActionFunc(name, evt.action, stateType)
		if err != nil {
			return nil, err
		}
		em.callbacks[name] = callback{
			argumentTypes: argumentTypes,
			action:        evt.action,
		}
		for src, dst := range evt.transitionsSoFar {
			if dst != nil && !reflect.TypeOf(dst).AssignableTo(stateFieldType.Type) {
				return nil, xerrors.Errorf("event `%+v` destination type is not assignable to: %s", name, stateFieldType.Type.Name())
			}
			if src != nil && !reflect.TypeOf(src).AssignableTo(stateFieldType.Type) {
				return nil, xerrors.Errorf("event `%+v` source type is not assignable to: %s", name, stateFieldType.Type.Name())
			}
			em.transitions[eKey{name, src}] = dst
		}
	}
	return em, nil
}

// Event generates an event that can be dispatched to go-statemachine from the given event name and context args
func (em eventProcessor) Generate(ctx context.Context, event EventName, returnChannel chan error, args ...interface{}) (interface{}, error) {
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
	userValue := reflect.ValueOf(user)
	currentState := userValue.Elem().FieldByName(string(em.stateKeyField)).Interface()
	e, ok := evt.User.(fsmEvent)
	if !ok {
		return nil, xerrors.New("Not an fsm event")
	}
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

func inspectActionFunc(name EventName, action ActionFunc, stateType reflect.Type) ([]reflect.Type, error) {
	if action == nil {
		return nil, nil
	}

	atType := reflect.TypeOf(action)
	if atType.Kind() != reflect.Func {
		return nil, xerrors.Errorf("event `%+v` has a callback that is not a function", name)
	}
	if atType.NumIn() < 1 {
		return nil, xerrors.Errorf("event `%+v` has a callback that does not take the state", name)
	}
	if !reflect.PtrTo(stateType).AssignableTo(atType.In(0)) {
		return nil, xerrors.Errorf("event `%+v` has a callback that does not take the state", name)
	}
	if atType.NumOut() != 1 || atType.Out(0).AssignableTo(reflect.TypeOf(new(error))) {
		return nil, xerrors.Errorf("event `%+v` callback should return exactly one param that is an error", name)
	}
	argumentTypes := make([]reflect.Type, atType.NumIn()-1)
	for i := range argumentTypes {
		argumentTypes[i] = atType.In(i + 1)
	}
	return argumentTypes, nil
}
