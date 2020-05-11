# go-statemachine FSM Module

A well defined DSL for constructing finite state machines for use in Filecoin.


## Table of Contents

* [Background Reading](./README.md#background-reading)
* [Description](./README.md#description)
* [Usage](./README.md#usage)
* [Interaction With Statestore](./README.md#interaction-with-statestore)
* [License](./README.md#license)

## Background Reading

[Wikipedia - Finite State Machines](https://en.wikipedia.org/wiki/Finite-state_machine)
[Wikipedia - UML State Machine](https://en.wikipedia.org/wiki/UML_state_machine)

## Description

This library provides a way to model a Filecoin process that operates on a specific data structure as a series of state transitions. It is used in the storage and retrieval markets in [go-fil-markets repository](https://github.com/filecoin-project/go-fil-markets) to model the lifecycle of a storage or retrieval deal. It may be used for other parts of Filecoin in the future.

A state machine is defined in terms of

- `StateType` -- the type of data structure we are tracking (should be a struct)
- `StateKeyField` -- the field in the data structure that represents the unique identifier for the current state. Must be a field that is comparable in golang (i.e. not an array or map)
- `Events` -- Events are the list of inputs that trigger transitions in state. An event is defined in terms of:
  - An identifier
  - A mapping between source states and destination states -- events can apply to one, many, or all source states. When there is more than one source state, the source states can share or have different destination states
  - An `Action` - this a function that applies updates to the underlying data structure in fields that are not `StateKeyField`
- `StateEntryFuncs` - State entry functions are handlers that get called when the FSM enters a specific state. Where Actions are associated with specific events, state entry funcs are associated with specific states. Actions can modify the underlying data structure. StateEntryFuncs can only trigger additional events if they wish to modify the state (a state entry func receives a dereferenced value for the data structure, rather than the pointer)
- `Environment` - This is a single interface that is used to access external dependencies. It is available to a `StateEntryFunc`
- `Notifier` - A function that gets called on each successfully applied state transition, before any state entry func is called. This is useful for providing external notifications about state updates. It is called with the name of the event applied and the current state of the data structure.
- `FinalityStates` - a list of states from which the state machine cannot leave. When the statemachine enters these states, it shuts down and stops receiving messages.

## Usage

Let's consider a hypothetical deal we want to track in Filecoin. Each deal will have a series of states it can be in, and various things that can happen to change its state. We will track multiple deals at the same time. In this example, we will model the actions of the receiving party (i.e. the person who accepted the deal and is responding)

Here' is the simplified deal structure:

```golang
type DealState struct {
  // the original proposal
  DealProposal
  // the current status identifier
  Status          DealStatus
  // the other party's network address
  Receiver        peer.ID
  // how much much we received
  TotalSent       uint64
  // how much money we have
	FundsReceived   abi.TokenAmount
  // an informational message to augment the status of the deal
  Message         string
}
```

Let's pretend our ideal deal flow looks like:

```
Receive new deal proposal -> Validate proposal -> For All Data Requested, Send a chunk, then request payment, then wait for payment before sending more -> Complete deal
```

You can imagine at each state in this happy path, things could go wrong. A deal proposal might need to be rejected for not meeting criteria. A network message could fail to send. A client could send only a partial payment, or one that fails to process. We might had an error reading our own data.

You can start to assemble different events that might happen in this process:

```
ReceivedNewDeal
AcceptedDeal
RejectedDeal
SentData
RequestedPayment
ReceivedPayment
...
```

And you can imagine different states the deal is in:

```
New
Accepted
Rejected
AwaitingPayment
Completed
Failed
...
```

We would track these states in the `Status` field of our deal.

So we have `StateType` - `DealState` - and a `StateKeyField` - `Status`. Now we need to define our events. Here's how we do that, using the modules custom DSL:

```golang
var DealEvents = fsm.Events{
  fsm.Event("ReceivedNewDeal").FromAny().To("New").Action(func (deal * DealState, proposal DealProposal) error {
    deal.DealProposal = proposal
    return nil
  }),
  fsm.Event("RejectedDeal").From("New").To("Rejected").Action(func (deal * DealState, reason string) error {
    deal.Message = fmt.Sprintf("Rejected deal because: %s", reason)
    return nil
  }),
  fsm.Event("AcceptedDeal").From("New").To("Accepted"),
  ...
}
```

As we enter each new state, there are things we need to do to advance the deal flow. When we have a new deal proposal, we need to validate it, to accept or reject. When we accept a deal, we need to start sending data. This is handled by `StateEntryFuncs`. We define this as a mapping between states and their handlers:

```golang
// assuming ValidateDeal, SendData, and SendRejection are defined elsewhere
var DealEntryFuncs = fsm.StateEntryFuncs{
  "New": ValidateDeal,
  "Rejected": SendRejection
  "Accepted": SendData,
}
```

As a baseline rule, where `Actions` on `Events` modify state, `StateEntryFuncs` leave state as it is but can have side effects. We therefore define an `Environment` type used to trigger these side effects with the outside world. We might define our environment here as:

```golang
type DealResponse struct {
  Data: []byte
}

type DealEnvironment interface {
  LookUpSomeData(proposal DealProposal) ([]byte, err)
  SendDealResponse(response DealResponse) error
  ...
}
```

And then our SendData `StateEntryFunc` function might look like this:

```golang
func SendData(ctx fsm.Context, environment DealEnvironment, deal DealState) error {
  data, err := environment.LookupSomeData(deal.DealProposal)
  if err != nil {
    return ctx.Trigger("DataError", err)
  }

  err := environment.SendDealResponse(DealResponse{Data: data})
  if err != nil {
    return ctx.Trigger("NetworkError", err)
  }

  return ctx.Trigger("SentData", len(data))
}
```

You can see our SendData interacts with the environment dispatches additional events using the `Trigger` function on the FSM Context which is also supplied to a `StateEntryFunc`. While the above function only dispatches at most one new event based on the conditional logic, it is possible to dismatch multiple events in a state entry func and they will be processed asynchronously in the order they are received.

Putting all this together, our state machine parameters are as follows:

```golang
var DealFSMParameters = fsm.Parameters{
  StateType: DealState{},
  StateKeyField: "Status",
  Events: DealEvents,
  StateEntryFuncs: DealEntryFuncs,
  Environment: DealEnvironmentImplementation{}
  FinalityStates: []StateKey{"Completed", "Failed"} 
}
```

## Interaction with statestore

The FSM module is designed to be used with a list of data structures, persisted to a datastore through the `go-statestore` module.

You initialize a new set of FSM's as follows:

```golang
var ds datastore.Batching

var dealStateMachines = fsm.New(ds, DealFSMParameters)
```

You can now dispatch events from the outside to a particular deal FSM with:

```golang
var DealID // some identifier of a deal
var proposal DealProposal
dealStateMachines.Send(DealID, "ReceivedNewDeal", proposal)
```

The statemachine will initialize a new FSM if it is not already tracking data for the given identifier (the first parameter -- in this case a deal ID)

That's it! The FSM module:

- persists state machines to disk with `go-statestore`
- operated asynchronously and in nonblocking fashion (any event dispatches will return immediate)
- operates multiple FSMs in parallel (each machine has its own go-routine)

## License

This module is dual-licensed under Apache 2.0 and MIT terms.

Copyright 2019. Protocol Labs, Inc.
