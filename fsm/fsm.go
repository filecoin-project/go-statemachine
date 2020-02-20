package fsm

import (
	"context"
	"reflect"

	"github.com/filecoin-project/go-statemachine"
	logging "github.com/ipfs/go-log"
	"golang.org/x/xerrors"
)

var log = logging.Logger("fsm")

type fsmHandler struct {
	stateType     reflect.Type
	stateKeyField StateKeyField
	notifier      Notifier
	callbacks     map[EventName]callback
	transitions   map[eKey]StateKey
	stateHandlers StateHandlers
	environment   Environment
}

// NewFSMHandler defines an StateHandler for go-statemachine that implements
// a traditional Finite State Machine model -- transitions, start states,
// end states, and callbacks
func NewFSMHandler(parameters Parameters) (statemachine.StateHandler, error) {
	environmentType := reflect.TypeOf(parameters.Environment)
	stateType := reflect.TypeOf(parameters.StateType)
	stateFieldType, ok := stateType.FieldByName(string(parameters.StateKeyField))
	if !ok {
		return nil, xerrors.Errorf("state type has no field `%s`", parameters.StateKeyField)
	}
	if !stateFieldType.Type.Comparable() {
		return nil, xerrors.Errorf("state field `%s` is not comparable", parameters.StateKeyField)
	}

	d := fsmHandler{
		environment:   parameters.Environment,
		stateType:     stateType,
		stateKeyField: parameters.StateKeyField,
		callbacks:     make(map[EventName]callback),
		transitions:   make(map[eKey]StateKey),
		stateHandlers: make(StateHandlers),
		notifier:      parameters.Notifier,
	}

	// Build transition map and store sets of all events and states.
	for name, desc := range parameters.Events {
		argumentTypes, err := inspectApplyTransitionFunc(name, desc, stateType)
		if err != nil {
			return nil, err
		}
		d.callbacks[name] = callback{
			argumentTypes:   argumentTypes,
			applyTransition: desc.ApplyTransition,
		}
		for src, dst := range desc.TransitionMap {

			if dst != nil && !reflect.TypeOf(dst).AssignableTo(stateFieldType.Type) {
				return nil, xerrors.Errorf("event `%+v` destination type is not assignable to: %s", name, stateFieldType.Type.Name())
			}
			if dst != nil {
				d.stateHandlers[dst] = nil
			}
			if src != nil && !reflect.TypeOf(src).AssignableTo(stateFieldType.Type) {
				return nil, xerrors.Errorf("event `%+v` source type is not assignable to: %s", name, stateFieldType.Type.Name())
			}
			d.transitions[eKey{name, src}] = dst
			if src != nil {
				d.stateHandlers[src] = nil
			}
		}
	}

	// type check state handlers
	for state, stateHandler := range parameters.StateHandlers {
		if !reflect.TypeOf(state).AssignableTo(stateFieldType.Type) {
			return nil, xerrors.Errorf("state key is not assignable to: %s", stateFieldType.Type.Name())
		}
		expectedHandlerType := reflect.FuncOf([]reflect.Type{reflect.TypeOf((*Context)(nil)).Elem(), environmentType, d.stateType}, []reflect.Type{reflect.TypeOf(new(error)).Elem()}, false)
		validHandler := expectedHandlerType.AssignableTo(reflect.TypeOf(stateHandler))
		if !validHandler {
			return nil, xerrors.Errorf("handler for state does not match expected type")
		}
		d.stateHandlers[state] = stateHandler
	}

	return d, nil
}

func (d fsmHandler) completePlan(event fsmEvent, handler interface{}, processed uint64, err error) (interface{}, uint64, error) {
	if event.returnChannel != nil {
		select {
		case <-event.ctx.Done():
		case event.returnChannel <- err:
		}
	}
	// we drop the error so from the state machine's point of view this event is processed
	if err != nil {
		log.Errorf("Executing event planner failed: %+v", err)
	}
	return handler, processed, nil
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
	currentState := userValue.Elem().FieldByName(string(d.stateKeyField)).Interface()
	e := events[0].User.(fsmEvent)
	destination, ok := d.transitions[eKey{e.name, currentState}]
	// check for fallback transition for any source state
	if !ok {
		destination, ok = d.transitions[eKey{e.name, nil}]
	}
	if !ok {
		return d.completePlan(e, nil, 1, xerrors.Errorf("Invalid transition in queue, state `%+v`, event `%+v`", currentState, e.name))
	}
	cb := d.callbacks[e.name]
	err := d.applyTransition(userValue, e, cb)
	if err != nil {
		return d.completePlan(e, nil, 1, err)
	}

	if destination != nil {
		userValue.Elem().FieldByName(string(d.stateKeyField)).Set(reflect.ValueOf(destination))
		currentState = destination
	}
	if d.notifier != nil {
		d.notifier(e.name, userValue.Elem().Interface())
	}
	return d.completePlan(e, d.handler(d.stateHandlers[currentState]), 1, nil)
}

func (d fsmHandler) applyTransition(userValue reflect.Value, e fsmEvent, cb callback) error {
	if cb.applyTransition == nil {
		return nil
	}
	values := make([]reflect.Value, 0, len(e.args)+1)
	values = append(values, userValue)
	for _, arg := range e.args {
		values = append(values, reflect.ValueOf(arg))
	}
	res := reflect.ValueOf(cb.applyTransition).Call(values)

	if res[0].Interface() != nil {
		return xerrors.Errorf("Error applying event transition `%+v`: %w", e.name, res[0].Interface().(error))
	}
	return nil
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

func (d fsmHandler) event(ctx context.Context, event EventName, returnChannel chan error, args ...interface{}) (fsmEvent, error) {
	cb, ok := d.callbacks[event]
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

// eKey is a struct key used for storing the transition map.
type eKey struct {
	// event is the name of the event that the keys refers to.
	event EventName

	// src is the source from where the event can transition.
	src interface{}
}

type callback struct {
	argumentTypes   []reflect.Type
	applyTransition interface{}
}

type fsmContext struct {
	state interface{}
	ctx   statemachine.Context
	d     fsmHandler
}

func (dc fsmContext) Context() context.Context {
	return dc.ctx.Context()
}

func (dc fsmContext) Event(event EventName, args ...interface{}) error {
	evt, err := dc.d.event(dc.ctx.Context(), event, nil, args...)
	if err != nil {
		return err
	}
	return dc.ctx.Send(evt)
}

var _ Context = fsmContext{}

type fsmEvent struct {
	name          EventName
	args          []interface{}
	ctx           context.Context
	returnChannel chan error
}

func inspectApplyTransitionFunc(name EventName, e EventDesc, stateType reflect.Type) ([]reflect.Type, error) {
	if e.ApplyTransition == nil {
		return nil, nil
	}

	atType := reflect.TypeOf(e.ApplyTransition)
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
