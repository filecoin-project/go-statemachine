package fsm

// EventDesc describes what happens when an event is triggered
//
// The event description contains a map of eligible source states
// to destination states
// It also contains the specified transition function
// to make additional modifications to the internal state.
// Transition map is a map of source state to destination state when the event
// is triggered
// a destination of nil means no change in state, but the state handler will
// be triggered again

// ApplyTransition is a function to make additional modifications to state
// based on the event

// TransitionToBuilder sets the destination to complete a transition
type TransitionToBuilder interface {
	To(StateKey) EventBuilder
	ToNoChange() EventBuilder
}

type transitionToBuilder struct {
	name             EventName
	applyTransition  ApplyTransitionFunc
	transitionsSoFar map[StateKey]StateKey
	nextFrom         []StateKey
}

func (t transitionToBuilder) To(to StateKey) EventBuilder {
	transitions := t.transitionsSoFar
	for _, from := range t.nextFrom {
		transitions[from] = to
	}
	return EventBuilder{t.name, t.applyTransition, transitions}
}

func (t transitionToBuilder) ToNoChange() EventBuilder {
	transitions := t.transitionsSoFar
	for _, from := range t.nextFrom {
		transitions[from] = nil
	}
	return EventBuilder{t.name, t.applyTransition, transitions}
}

type EventBuilder struct {
	name             EventName
	applyTransition  ApplyTransitionFunc
	transitionsSoFar map[StateKey]StateKey
}

func (t EventBuilder) From(s StateKey) TransitionToBuilder {
	return transitionToBuilder{
		t.name,
		t.applyTransition,
		t.transitionsSoFar,
		[]StateKey{s},
	}
}

func (t EventBuilder) FromAny() TransitionToBuilder {
	return transitionToBuilder{
		t.name,
		t.applyTransition,
		t.transitionsSoFar,
		[]StateKey{nil},
	}
}

func (t EventBuilder) FromMany(sources ...StateKey) TransitionToBuilder {
	return transitionToBuilder{
		t.name,
		t.applyTransition,
		t.transitionsSoFar,
		sources,
	}
}

func (t EventBuilder) WithCallback(applyTransition ApplyTransitionFunc) EventBuilder {
	return EventBuilder{
		t.name,
		applyTransition,
		t.transitionsSoFar,
	}
}

func Event(name EventName) EventBuilder {
	return EventBuilder{name, nil, map[StateKey]StateKey{}}
}
