package fsm

import (
	"reflect"

	"golang.org/x/xerrors"
)

// VerifyStateParameters verifies if the Parameters for an FSM specification are sound
func VerifyStateParameters(parameters Parameters) error {
	environmentType := reflect.TypeOf(parameters.Environment)
	stateType := reflect.TypeOf(parameters.StateType)
	stateFieldType, ok := stateType.FieldByName(string(parameters.StateKeyField))
	if !ok {
		return xerrors.Errorf("state type has no field `%s`", parameters.StateKeyField)
	}
	if !stateFieldType.Type.Comparable() {
		return xerrors.Errorf("state field `%s` is not comparable", parameters.StateKeyField)
	}

	// type check state handlers
	for state, stateEntryFunc := range parameters.StateEntryFuncs {
		if !reflect.TypeOf(state).AssignableTo(stateFieldType.Type) {
			return xerrors.Errorf("state key is not assignable to: %s", stateFieldType.Type.Name())
		}
		err := inspectStateEntryFunc(stateEntryFunc, environmentType, stateType)
		if err != nil {
			return err
		}
	}
	return nil
}

func VerifyEventParameters(state StateType, stateKeyField StateKeyField, events []EventBuilder) error {
	stateType := reflect.TypeOf(state)
	stateFieldType, ok := stateType.FieldByName(string(stateKeyField))
	if !ok {
		return xerrors.Errorf("state type has no field `%s`", stateKeyField)
	}
	if !stateFieldType.Type.Comparable() {
		return xerrors.Errorf("state field `%s` is not comparable", stateKeyField)
	}
	callbacks := map[EventName]struct{}{}

	// Build transition map and store sets of all events and states.
	for _, evtIface := range events {
		evt, ok := evtIface.(eventBuilder)
		if !ok {
			errEvt := evtIface.(errBuilder)
			return errEvt.err
		}

		name := evt.name

		_, exists := callbacks[name]
		if exists {
			return xerrors.Errorf("Duplicate event name `%+v`", name)
		}

		err := inspectActionFunc(name, evt.action, stateType)
		if err != nil {
			return err
		}
		for src, dst := range evt.transitionsSoFar {
			_, justRecord := dst.(recordEvent)
			if dst != nil && !justRecord && !reflect.TypeOf(dst).AssignableTo(stateFieldType.Type) {
				return xerrors.Errorf("event `%+v` destination type is not assignable to: %s", name, stateFieldType.Type.Name())
			}
			if src != nil && !reflect.TypeOf(src).AssignableTo(stateFieldType.Type) {
				return xerrors.Errorf("event `%+v` source type is not assignable to: %s", name, stateFieldType.Type.Name())
			}
		}
	}
	return nil
}

func inspectActionFunc(name EventName, action ActionFunc, stateType reflect.Type) error {
	if action == nil {
		return nil
	}

	atType := reflect.TypeOf(action)
	if atType.Kind() != reflect.Func {
		return xerrors.Errorf("event `%+v` has a callback that is not a function", name)
	}
	if atType.NumIn() < 1 {
		return xerrors.Errorf("event `%+v` has a callback that does not take the state", name)
	}
	if !reflect.PtrTo(stateType).AssignableTo(atType.In(0)) {
		return xerrors.Errorf("event `%+v` has a callback that does not take the state", name)
	}
	if atType.NumOut() != 1 || atType.Out(0).AssignableTo(reflect.TypeOf(new(error))) {
		return xerrors.Errorf("event `%+v` callback should return exactly one param that is an error", name)
	}
	return nil
}

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
