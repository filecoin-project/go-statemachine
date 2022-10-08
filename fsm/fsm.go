package fsm

import (
	"context"
	"reflect"
	"time"

	"github.com/filecoin-project/go-statemachine"
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("fsm")

type fsmHandler struct {
	stateType                        reflect.Type
	stateKeyField                    StateKeyField
	notifier                         Notifier
	notifications                    chan notification
	eventProcessor                   EventProcessor
	stateEntryFuncs                  StateEntryFuncs
	environment                      Environment
	finalityStates                   map[StateKey]struct{}
	consumeAllEventsBeforeEntryFuncs bool
}

const NotificationQueueSize = 128

type notification struct {
	eventName EventName
	state     StateType
}

// NewFSMHandler defines an StateHandler for go-statemachine that implements
// a traditional Finite State Machine model -- transitions, start states,
// end states, and callbacks
func NewFSMHandler(parameters Parameters) (statemachine.StateHandler, error) {
	err := VerifyStateParameters(parameters)
	if err != nil {
		return nil, err
	}
	stateType := reflect.TypeOf(parameters.StateType)
	eventProcessor, err := NewEventProcessor(parameters.StateType, parameters.StateKeyField, parameters.Events)
	if err != nil {
		return nil, err
	}
	d := fsmHandler{
		environment:                      parameters.Environment,
		stateType:                        stateType,
		stateKeyField:                    parameters.StateKeyField,
		eventProcessor:                   eventProcessor,
		stateEntryFuncs:                  make(StateEntryFuncs),
		notifier:                         parameters.Notifier,
		finalityStates:                   make(map[StateKey]struct{}),
		consumeAllEventsBeforeEntryFuncs: parameters.Options.ConsumeAllEventsBeforeEntryFuncs,
	}

	// type check state handlers
	for state, stateEntryFunc := range parameters.StateEntryFuncs {
		d.stateEntryFuncs[state] = stateEntryFunc
	}

	for _, finalityState := range parameters.FinalityStates {
		d.finalityStates[finalityState] = struct{}{}
	}

	if d.notifier != nil {
		d.notifications = make(chan notification)
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
	userValue := reflect.ValueOf(user)
	// if we reach a finality state, we do not process events
	if d.reachedFinalityState(userValue.Elem().Interface()) {
		d.eventProcessor.ClearEvents(events, statemachine.ErrTerminated)
		return nil, uint64(len(events)), statemachine.ErrTerminated
	}
	// in normal mode, we consume exactly one event per call to Plan,
	// meaning state entry funcs are immediately processed
	processedEventTarget := 1
	// in consume all events mode, we attempt to process every event in each call to Plan,
	// meaning state entry funcs are processed only when all waiting events are consumed
	if d.consumeAllEventsBeforeEntryFuncs {
		processedEventTarget = len(events)
	}
	var runHandler bool
	var currentState interface{}
	// for each event up to the target
	for i := 0; i < processedEventTarget; i++ {
		// process the event
		eventName, err := d.eventProcessor.Apply(events[i], user)
		// internally the fsm creates private "nil events" to support
		// synchronization
		if _, ok := eventName.(nilEvent); ok {
			continue
		}
		_, skipHandler := err.(ErrSkipHandler)
		// terminate if there is a non-skip error
		if err != nil && !skipHandler {
			log.Errorf("Executing event planner failed for %s: %+v", d.stateType.Name(), err)
			return nil, uint64(i + 1), nil
		}
		// when processing multiple events, we run the handler unless all events
		// return ErrSkipHandler (i.e. ToJustRecord)
		if !skipHandler {
			runHandler = true
		}
		// read the state
		currentState = userValue.Elem().FieldByName(string(d.stateKeyField)).Interface()

		// dispatch notification
		if d.notifier != nil {
			currentTime := time.Now()
			d.notifications <- notification{eventName, userValue.Elem().Interface()}
			elapsed := time.Since(currentTime)
			if elapsed > maxExpectedEventProcessingTime {
				log.Warnw("notification queued", "fsmType", d.stateType.Name(), "eventName", eventName, "queueTime", elapsed)
			} else {
				log.Debugw("notification queued", "fsmType", d.stateType.Name(), "eventName", eventName, "queueTime", elapsed)
			}
		}
		// if we're now in finality, return
		_, final := d.finalityStates[currentState]
		if final {
			d.eventProcessor.ClearEvents(events[i+1:], statemachine.ErrTerminated)
			return nil, uint64(len(events)), statemachine.ErrTerminated
		}
	}

	// run the state entry func as needed
	if !runHandler {
		return d.handler(nil), uint64(processedEventTarget), nil
	}
	return d.handler(d.stateEntryFuncs[currentState]), uint64(processedEventTarget), nil
}

func (d fsmHandler) reachedFinalityState(user interface{}) bool {
	userValue := reflect.ValueOf(user)
	currentState := userValue.FieldByName(string(d.stateKeyField)).Interface()
	_, final := d.finalityStates[currentState]
	return final
}

const maxExpectedEventProcessingTime = 50 * time.Millisecond

// Init will start up a goroutine which processes the notification queue
// in order
func (d fsmHandler) Init(closing <-chan struct{}) {
	if d.notifier != nil {
		queue := make([]notification, 0, NotificationQueueSize)
		toProcess := make(chan notification)
		go func() {
			for {
				select {
				case n := <-toProcess:
					startTime := time.Now()
					d.notifier(n.eventName, n.state)
					processingTime := time.Since(startTime)
					if processingTime > maxExpectedEventProcessingTime {
						log.Warnw("fsm listeners processed", "fsmType", d.stateType.Name(), "eventName", n.eventName, "processingTime", processingTime)
					} else {
						log.Debugw("fsm listeners processed", "fsmType", d.stateType.Name(), "eventName", n.eventName, "processingTime", processingTime)
					}
				case <-closing:
					return
				}
			}
		}()
		go func() {
			outgoing := func() chan<- notification {
				if len(queue) == 0 {
					return nil
				}
				return toProcess
			}
			nextNotification := func() notification {
				if len(queue) == 0 {
					return notification{}
				}
				return queue[0]
			}
			for {
				select {
				case n := <-d.notifications:
					queue = append(queue, n)
					log.Debugw("unprocessed state machine notifications", "fsmType", d.stateType.Name(), "total", len(queue))
				case outgoing() <- nextNotification():
					queue = queue[1:]
				case <-closing:
					return
				}
			}
		}()
	}
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
