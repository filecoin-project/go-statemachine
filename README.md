# go-statemachine

A generic state machine

## Table of Contents

* [Description](./README.md#description)
* [Usage](./README.md#usage)
* [Interation With Statestore](./README.md#interaction-with-statestore)
* [License](./README.md#license)

## Description

The module provides tools for defining and tracking generic state machines. For a more specific implementation that specifically defines finite state machines, see the FSM module, that provides additional tools on top of the generic machines defined here

State machines:
- Receive Events
- Modify State
- Take further actions based on new state, possibly generating more events

## Usage

A state machine is defined in terms of a Planner.

It has the following signature:

```golang
type Planner func(events []Event, user interface{}) (nextActions interface{}, eventsProcessed uint64, err error)
```

A planner receives a series of events and a pointer to the current state (represented by user)

The planner generally:
- processes one or more events that mutate state
- constructs a function that will perform additional actions based on the new state

It returns:
1. The next actions handler-- should have the signature func(ctx Context, st <T>) error), where <T> is the typeOf(user) param
2. The number of events processed
3. An error if occured

As a general rule, you mutate **inside** the planner function, while you perform side effects **outside** the planner in the return handler, which only receives a value for state and cannot mutate it (but can dispatch more events).

## Interaction with statestore

The `go-statemachine` is designed to be used with a list of data structures, persisted to a datastore through the `go-statestore` module. When working with statemachines this way, you define a StateHandler with a Plan function that works like a Planner.

You initialize a new set of statemachines for a given type of state as follows:

```golang
var ds datastore.Batching
var stateHandler StateHandler

var stateMachines = statemachine.New(ds, stateHandler, StateType{})
```

This creates a machine for the given data store that will process data of type `StateType` with the given stateHandler.

You can now dispatch events to a state machine with:

```golang
var id interface{} // some identifier of a tracked state
var evt Event // some event type
stateMachines.Send(id, evt)
```

The state machine group will initialize a new statemachine if it is not already tracking data for the given identifier

## License

Dual-licensed under [MIT](https://github.com/filecoin-project/go-statemachine/blob/master/LICENSE-MIT) + [Apache 2.0](https://github.com/filecoin-project/go-statemachine/blob/master/LICENSE-APACHE)
