package testbus

import (
	"context"

	eventbus "github.com/protocol/hack-the-bus"
)

type TestCollection struct {
}

var t eventbus.Collection

// CreateNewBus creates a new Bus and returns its UUID
// It takes a list of policies under which it will declare a consumer "failed"
func (tc *TestCollection) CreateNewBus(failurePolicy []eventbus.FailureCondition) (eventbus.Bus, error) {
	return &testBus{eventsIn: make(chan eventbus.NextEvents)}, nil
}

// GetByUUID returns the bus with the given UUID
func (tc *TestCollection) GetByUUID(_ eventbus.UUID) (eventbus.Bus, error) {
	panic("not implemented") // TODO: Implement
}

// GetBusByParticipant find the bus that contains the unique particpant
// (useful for relocating a bus without having to find a UUID)
func (tc *TestCollection) GetBusByParticpant(_ eventbus.Participant) (eventbus.Bus, error) {
	return nil, nil
}

// AddObserver adds an observer for the given event types across all buses.
// Observers have different characteristics to consumers
// They simply receive periodic updates about events that have happened
// The only guarantees are:
// - they receive events in order groups per bus (no guarantee about ordering between buses)
// - they ALWAYS receive events after ALL consumers consume or skip them
// For AddObserver, it only observes events published after it's added
func (tc *TestCollection) AddObserver(eventTypes []eventbus.EventType, observer eventbus.Observer) error {
	panic("not implemented") // TODO: Implement
}

// DeleteBus shuts down a bus. Since buses are relatively transient (length of a transfer), we should be able
// to shut down and throw away a bus and all its events. Any existing ProducerConsumer/Producer/Consumer instances
// will just return errors on their methods
func (tc *TestCollection) DeleteBus(_ context.Context, _ eventbus.UUID) error {
	panic("not implemented") // TODO: Implement
}

// SetDefaultFailureHandler sets the default failure handler for all newly created buses
func (tc *TestCollection) SetDefaultFailureHandler(_ eventbus.FailureHandler) {
	panic("not implemented") // TODO: Implement
}

type testBus struct {
	eventsIn chan eventbus.NextEvents
}

func (tb *testBus) UUID() {
	panic("not implemented") // TODO: Implement
}

func (tb *testBus) CreateProducer(_ eventbus.Participant) (eventbus.Producer, error) {
	return &testProducer{tb.eventsIn}, nil
}
func (tb *testBus) CreateConsumer(_ eventbus.Participant) (eventbus.Consumer, error) {
	return &testConsumer{tb.eventsIn}, nil
}
func (tb *testBus) LookupProducer(_ eventbus.Participant) (eventbus.Producer, error) {
	panic("not implemented") // TODO: Implement
}

func (tb *testBus) LookupConsumer(_ eventbus.Participant) (eventbus.Consumer, error) {
	panic("not implemented") // TODO: Implement
}

func (tb *testBus) AddObserver(eventTypes []eventbus.EventType, observer eventbus.Observer) error {
	panic("not implemented") // TODO: Implement
}

func (tb *testBus) SetFailureHandler(_ eventbus.FailureHandler) {
	panic("not implemented") // TODO: Implement
}

type testProducer struct {
	eventsIn chan eventbus.NextEvents
}

func (tp *testProducer) Participant() eventbus.Participant {
	panic("not implemented") // TODO: Implement
}

func (tp *testProducer) Bus() eventbus.Bus {
	panic("not implemented") // TODO: Implement
}

func (tp *testProducer) PublishEvent(eventType eventbus.EventType, eventData eventbus.EventData) <-chan error {
	errChan := make(chan error, 1)
	go func() {
		tp.eventsIn <- &testNextEvents{eventType: eventType, eventData: eventData}
		errChan <- nil
	}()
	return errChan
}

type testNextEvents struct {
	eventType eventbus.EventType
	eventData eventbus.EventData
}

func (tne *testNextEvents) Events() []eventbus.Event {
	return []eventbus.Event{&testEvent{tne.eventType, tne.eventData}}
}

func (tne *testNextEvents) Error() error {
	return nil
}

type testEvent struct {
	eventType eventbus.EventType
	eventData eventbus.EventData
}

func (te *testEvent) BusUUID() eventbus.UUID {
	panic("not implemented") // TODO: Implement
}

func (te *testEvent) Position() eventbus.Position {
	panic("not implemented") // TODO: Implement
}

func (te *testEvent) Publisher() eventbus.Participant {
	panic("not implemented") // TODO: Implement
}

func (te *testEvent) Type() eventbus.EventType {
	return te.eventType
}

func (te *testEvent) Data() eventbus.EventData {
	return te.eventData
}

// PublishSynchronousEvent publishes an event an sets it up as a synchronization checkpoint
// It will return two channels -- one that emits first when the event is
// published, and one that emits when when the synchronization is complete
// or has errored with a final error status
func (tp *testProducer) PublishSynchronousEvent(_ eventbus.EventType, _ eventbus.EventData) (published <-chan error, synchrononized <-chan error) {
	panic("not implemented") // TODO: Implement
}

type testConsumer struct {
	eventsIn chan eventbus.NextEvents
}

var c eventbus.BusParticipant

// ParticipantID returns the ID of this participant
func (tc *testConsumer) Participant() eventbus.Participant {
	panic("not implemented") // TODO: Implement
}

func (tc *testConsumer) Bus() eventbus.Bus {
	panic("not implemented") // TODO: Implement
}

// AddEventInterests adds events types that this consumer is interested in.
// Events published prior to the current queue position are NOT
// affected
func (tc *testConsumer) AddEventInterests(_ []eventbus.EventType) error {
	return nil
}

// ConsumedPosition is the position of events already consumed
func (tc *testConsumer) ConsumedPosition() eventbus.Position {
	panic("not implemented") // TODO: Implement
}

// Position indicates the position in the queue after the last call to
// NextAvailableEvents
func (tc *testConsumer) Position() eventbus.Position {
	panic("not implemented") // TODO: Implement
}

// NextAvailableEvents polls the event queue for more available events and returns them
// once they are available.
// If any events are available, they will be returned immediately
// The next call to NextEvents will return where the last call left off
func (tc *testConsumer) NextEvents() <-chan eventbus.NextEvents {
	return tc.eventsIn
}

// ConsumedEvents marks the consumer as having processed events.
func (tc *testConsumer) ConsumeEvents(offset eventbus.Offset) error {
	return nil
}

// RemoveEventInterests adds event types that this consumer is interested in.
// Events published prior to the current queue position are NOT
// affected
func (tc *testConsumer) RemoveEventInterests(_ []eventbus.EventType) error {
	panic("not implemented") // TODO: Implement
}
