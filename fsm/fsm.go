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
	stateType          reflect.Type
	stateField         StateKeyField
	transitionsByEvent map[EventName]eventDestination
	transitions        map[eKey]eventDestination
	stateHandlers      StateHandlers
	world              World
	handler            interface{}
}

// NewFSMHandler defines an StateHandler for go-statemachine that implements
// a traditional Finite State Machine model -- transitions, start states,
// end states, and callbacks
func NewFSMHandler(world World, state StateType, stateField StateKeyField, events []EventDesc, stateHandlers StateHandlers) (statemachine.StateHandler, error) {
	worldType := reflect.TypeOf(world)
	stateType := reflect.TypeOf(state)
	stateFieldType, ok := stateType.FieldByName(string(stateField))
	if !ok {
		return nil, xerrors.Errorf("state type has no field `%s`", stateField)
	}
	if !stateFieldType.Type.Comparable() {
		return nil, xerrors.Errorf("state field `%s` is not comparable", stateField)
	}

	d := fsmHandler{
		world:              world,
		stateType:          stateType,
		stateField:         stateField,
		transitionsByEvent: make(map[EventName]eventDestination),
		transitions:        make(map[eKey]eventDestination),
		stateHandlers:      make(StateHandlers),
	}

	// Build transition map and store sets of all events and states.
	for _, e := range events {
		if !reflect.TypeOf(e.Dst).AssignableTo(stateFieldType.Type) {
			return nil, xerrors.Errorf("event `%s` destination type is not assignable to: %s", e.Name, stateFieldType.Type.Name())
		}
		argumentTypes, err := inspectApplyTransitionFunc(e, stateType)
		if err != nil {
			return nil, err
		}
		destination := eventDestination{
			dst:             e.Dst,
			argumentTypes:   argumentTypes,
			applyTransition: e.ApplyTransition,
		}
		d.stateHandlers[e.Dst] = nil
		d.transitionsByEvent[e.Name] = destination
		for _, src := range e.Src {
			if !reflect.TypeOf(src).AssignableTo(stateFieldType.Type) {
				return nil, xerrors.Errorf("event `%s` source type is not assignable to: %s", e.Name, stateFieldType.Type.Name())
			}
			d.transitions[eKey{e.Name, src}] = destination
			d.stateHandlers[src] = nil
		}
	}

	// type check state handlers
	for state, stateHandler := range stateHandlers {
		if state != nil && !reflect.TypeOf(state).AssignableTo(stateFieldType.Type) {
			return nil, xerrors.Errorf("state key is not assignable to: %s", stateFieldType.Type.Name())
		}
		expectedHandlerType := reflect.FuncOf([]reflect.Type{reflect.TypeOf((*Context)(nil)).Elem(), worldType, d.stateType}, []reflect.Type{reflect.TypeOf(new(error)).Elem()}, false)
		validHandler := expectedHandlerType.AssignableTo(reflect.TypeOf(stateHandler))
		if !validHandler {
			return nil, xerrors.Errorf("handler for state does not match expected type")
		}
		d.stateHandlers[state] = stateHandler
	}

	d.handler = d.makeHandler()
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
// At the end it executes the specified handler for the final state,
// if specified
func (d fsmHandler) Plan(events []statemachine.Event, user interface{}) (interface{}, uint64, error) {
	userValue := reflect.ValueOf(user)
	currentState := userValue.Elem().FieldByName(string(d.stateField)).Interface()
	e := events[0].User.(fsmEvent)
	destination, ok := d.transitions[eKey{e.name, currentState}]
	if !ok {
		return d.completePlan(e, nil, 1, xerrors.Errorf("Invalid transition in queue, state `%+v`, event `%s`", currentState, e.name))
	}
	err := d.applyTransition(userValue, e, destination)
	if err != nil {
		return d.completePlan(e, nil, 1, err)
	}

	userValue.Elem().FieldByName(string(d.stateField)).Set(reflect.ValueOf(destination.dst))

	return d.completePlan(e, d.handler, 1, nil)
}

func (d fsmHandler) applyTransition(userValue reflect.Value, e fsmEvent, destination eventDestination) error {
	if destination.applyTransition == nil {
		return nil
	}
	values := make([]reflect.Value, 0, len(e.args)+1)
	values = append(values, userValue)
	for _, arg := range e.args {
		values = append(values, reflect.ValueOf(arg))
	}
	res := reflect.ValueOf(destination.applyTransition).Call(values)

	if res[0].Interface() != nil {
		return xerrors.Errorf("Error applying event transition `%s`: %w", e.name, res[0].Interface().(error))
	}
	return nil
}

func (d fsmHandler) makeHandler() interface{} {
	handlerType := reflect.FuncOf([]reflect.Type{reflect.TypeOf(statemachine.Context{}), d.stateType}, []reflect.Type{reflect.TypeOf(new(error)).Elem()}, false)

	call := func(cb interface{}, args []reflect.Value) []reflect.Value {
		ctx := args[0].Interface().(statemachine.Context)
		state := args[1].Interface()
		dContext := fsmContext{state, ctx, d}
		return reflect.ValueOf(cb).Call([]reflect.Value{reflect.ValueOf(dContext), reflect.ValueOf(d.world), args[1]})
	}

	baseHandler := reflect.MakeFunc(handlerType, func(args []reflect.Value) []reflect.Value {
		currentState := args[1].FieldByName(string(d.stateField)).Interface()
		cb := d.stateHandlers[currentState]
		if cb == nil {
			return []reflect.Value{reflect.ValueOf(error(nil))}
		}
		return call(cb, args)
	})

	universalHandler := d.stateHandlers[nil]
	if universalHandler == nil {
		return baseHandler.Interface()
	}

	return reflect.MakeFunc(handlerType, func(args []reflect.Value) []reflect.Value {
		results := call(universalHandler, args)
		if results[0].Interface() != nil {
			return results
		}
		return baseHandler.Call(args)
	}).Interface()
}

func (d fsmHandler) event(ctx context.Context, event EventName, returnChannel chan error, args ...interface{}) (fsmEvent, error) {
	destination, ok := d.transitionsByEvent[event]
	if !ok {
		return fsmEvent{}, xerrors.Errorf("Unknown event `%s`", event)
	}
	if len(args) != len(destination.argumentTypes) {
		return fsmEvent{}, xerrors.Errorf("Wrong number of arguments for event `%s`", event)
	}
	for i, arg := range args {
		if !reflect.TypeOf(arg).AssignableTo(destination.argumentTypes[i]) {
			return fsmEvent{}, xerrors.Errorf("Incorrect argument type at index `%d` for event `%s`", i, event)
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

type eventDestination struct {
	dst             interface{}
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

func inspectApplyTransitionFunc(e EventDesc, stateType reflect.Type) ([]reflect.Type, error) {
	if e.ApplyTransition == nil {
		return nil, nil
	}

	atType := reflect.TypeOf(e.ApplyTransition)
	if atType.Kind() != reflect.Func {
		return nil, xerrors.Errorf("event `%s` has a callback that is not a function", e.Name)
	}
	if atType.NumIn() < 1 {
		return nil, xerrors.Errorf("event `%s` has a callback that does not take the state", e.Name)
	}
	if !reflect.PtrTo(stateType).AssignableTo(atType.In(0)) {
		return nil, xerrors.Errorf("event `%s` has a callback that does not take the state", e.Name)
	}
	if atType.NumOut() != 1 || atType.Out(0).AssignableTo(reflect.TypeOf(new(error))) {
		return nil, xerrors.Errorf("event `%s` callback should return exactly one param that is an error", e.Name)
	}
	argumentTypes := make([]reflect.Type, atType.NumIn()-1)
	for i := range argumentTypes {
		argumentTypes[i] = atType.In(i + 1)
	}
	return argumentTypes, nil
}
