package fsm

import "golang.org/x/xerrors"

type recordEvent struct{}

type transitionToBuilder struct {
	name             EventName
	action           ActionFunc
	transitionsSoFar map[StateKey]StateKey
	nextFrom         []StateKey
}

// To means the transition ends in the given state
func (t transitionToBuilder) To(to StateKey) EventBuilder {
	transitions := t.transitionsSoFar
	for _, from := range t.nextFrom {
		transitions[from] = to
	}
	return eventBuilder{t.name, t.action, transitions}
}

// ToNoChange means a transition ends in the same state it started in (just retriggers state cb)
func (t transitionToBuilder) ToNoChange() EventBuilder {
	transitions := t.transitionsSoFar
	for _, from := range t.nextFrom {
		transitions[from] = nil
	}
	return eventBuilder{t.name, t.action, transitions}
}

// ToJustRecord means a transition ends in the same state it started in (and DOES NOT retrigger state cb)
func (t transitionToBuilder) ToJustRecord() EventBuilder {
	transitions := t.transitionsSoFar
	for _, from := range t.nextFrom {
		transitions[from] = recordEvent{}
	}
	return eventBuilder{t.name, t.action, transitions}
}

type eventBuilder struct {
	name             EventName
	action           ActionFunc
	transitionsSoFar map[StateKey]StateKey
}

// From begins describing a transition from a specific state
func (t eventBuilder) From(s StateKey) TransitionToBuilder {
	_, ok := t.transitionsSoFar[s]
	if ok {
		return errBuilder{t.name, xerrors.Errorf("duplicate transition source `%v` for event `%v`", s, t.name)}
	}
	return transitionToBuilder{
		t.name,
		t.action,
		t.transitionsSoFar,
		[]StateKey{s},
	}
}

// FromAny begins describing a transition from any state
func (t eventBuilder) FromAny() TransitionToBuilder {
	_, ok := t.transitionsSoFar[nil]
	if ok {
		return errBuilder{t.name, xerrors.Errorf("duplicate all-sources destination for event `%v`", t.name)}
	}
	return transitionToBuilder{
		t.name,
		t.action,
		t.transitionsSoFar,
		[]StateKey{nil},
	}
}

// FromMany begins describing a transition from many states
func (t eventBuilder) FromMany(sources ...StateKey) TransitionToBuilder {
	for _, source := range sources {
		_, ok := t.transitionsSoFar[source]
		if ok {
			return errBuilder{t.name, xerrors.Errorf("duplicate transition source `%v` for event `%v`", source, t.name)}
		}
	}
	return transitionToBuilder{
		t.name,
		t.action,
		t.transitionsSoFar,
		sources,
	}
}

// Action describes actions taken on the state for this event
func (t eventBuilder) Action(action ActionFunc) EventBuilder {
	if t.action != nil {
		return errBuilder{t.name, xerrors.Errorf("duplicate action for event `%v`", t.name)}
	}
	return eventBuilder{
		t.name,
		action,
		t.transitionsSoFar,
	}
}

type errBuilder struct {
	name EventName
	err  error
}

// From passes on the error
func (e errBuilder) From(s StateKey) TransitionToBuilder {
	return e
}

// FromAny passes on the error
func (e errBuilder) FromAny() TransitionToBuilder {
	return e
}

// FromMany passes on the error
func (e errBuilder) FromMany(sources ...StateKey) TransitionToBuilder {
	return e
}

// Action passes on the error
func (e errBuilder) Action(action ActionFunc) EventBuilder {
	return e
}

// To passes on the error
func (e errBuilder) To(_ StateKey) EventBuilder {
	return e
}

// ToNoChange passes on the error
func (e errBuilder) ToNoChange() EventBuilder {
	return e
}

// ToJustRecord passes on the error
func (e errBuilder) ToJustRecord() EventBuilder {
	return e
}

// Event starts building a new event
func Event(name EventName) EventBuilder {
	return eventBuilder{name, nil, map[StateKey]StateKey{}}
}
