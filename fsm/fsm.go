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
	stateType       reflect.Type
	stateKeyField   StateKeyField
	notifier        Notifier
	eventProcessor  EventProcessor
	stateEntryFuncs StateEntryFuncs
	environment     Environment
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
	}

	// type check state handlers
	for state, stateEntryFunc := range parameters.StateEntryFunc {
		if !reflect.TypeOf(state).AssignableTo(stateFieldType.Type) {
			return nil, xerrors.Errorf("state key is not assignable to: %s", stateFieldType.Type.Name())
		}
		err := inspectStateEntryFunc(stateEntryFunc, environmentType, d.stateType)
		if err != nil {
			return nil, err
		}
		d.stateEntryFuncs[state] = stateEntryFunc
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
	eventName, err := d.eventProcessor.Apply(events[0], user)
	if err != nil {
		log.Errorf("Executing event planner failed: %+v", err)
		return nil, 1, nil
	}
	userValue := reflect.ValueOf(user)
	currentState := userValue.Elem().FieldByName(string(d.stateKeyField)).Interface()
	if d.notifier != nil {
		d.notifier(eventName, userValue.Elem().Interface())
	}
	return d.handler(d.stateEntryFuncs[currentState]), 1, nil
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

func inspectStateEntryFunc(stateEntryFunc interface{}, environmentType reflect.Type, stateType reflect.Type) error {
	stateEntryFuncType := reflect.TypeOf(stateEntryFunc)
	if stateEntryFuncType.Kind() != reflect.Func {
		return xerrors.Errorf("handler for state is not a function")
	}
	if stateEntryFuncType.NumIn() != 3 {
		return xerrors.Errorf("handler for state does not take correct number of arguments")
	}
	if !reflect.TypeOf((*Context)(nil)).Elem().AssignableTo(stateEntryFuncType.In(0)) {
		return xerrors.Errorf("handler for state does not match context parameter")
	}
	if !environmentType.AssignableTo(stateEntryFuncType.In(1)) {
		return xerrors.Errorf("handler for state does not match environment parameter")
	}
	if !stateType.AssignableTo(stateEntryFuncType.In(2)) {
		return xerrors.Errorf("handler for state does not match state parameter")
	}
	if stateEntryFuncType.NumOut() != 1 || !stateEntryFuncType.Out(0).AssignableTo(reflect.TypeOf(new(error)).Elem()) {
		return xerrors.Errorf("handler for state does not return an error")
	}
	return nil
}
