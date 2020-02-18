package statemachine

import (
	"context"
	"testing"
	"time"

	"github.com/ipfs/go-datastore"
	logging "github.com/ipfs/go-log"
	"gotest.tools/assert"
)

func init() {
	logging.SetLogLevel("*", "INFO") // nolint: errcheck
}

type testHandler struct {
	t       *testing.T
	proceed chan struct{}
	done    chan struct{}
}

func (t *testHandler) Plan(events []Event, state interface{}) (interface{}, uint64, error) {
	return t.plan(events, state.(*TestState))
}

func (t *testHandler) plan(events []Event, state *TestState) (func(Context, TestState) error, uint64, error) {
	for _, event := range events {
		e := event.User.(*TestEvent)
		switch e.A {
		case "restart":
		case "start":
			state.A = 1
		case "b":
			state.A = 2
			state.B = e.Val
		}
	}

	switch state.A {
	case 1:
		return t.step0, uint64(len(events)), nil
	case 2:
		return t.step1, uint64(len(events)), nil
	default:
		t.t.Fatal(state.A)
	}
	panic("how?")
}

func (t *testHandler) step0(ctx Context, st TestState) error {
	ctx.Send(&TestEvent{A: "b", Val: 55}) // nolint: errcheck
	<-t.proceed
	return nil
}

func (t *testHandler) step1(ctx Context, st TestState) error {
	assert.Equal(t.t, uint64(2), st.A)

	close(t.done)
	return nil
}

func TestBasic(t *testing.T) {
	for i := 0; i < 1000; i++ { // run a few times to expose any races
		ds := datastore.NewMapDatastore()

		th := &testHandler{t: t, done: make(chan struct{}), proceed: make(chan struct{})}
		close(th.proceed)
		smm := New(ds, th, TestState{})

		if err := smm.Send(uint64(2), &TestEvent{A: "start"}); err != nil {
			t.Fatalf("%+v", err)
		}

		<-th.done
	}
}

func TestPersist(t *testing.T) {
	for i := 0; i < 1000; i++ { // run a few times to expose any races
		ds := datastore.NewMapDatastore()

		th := &testHandler{t: t, done: make(chan struct{}), proceed: make(chan struct{})}
		smm := New(ds, th, TestState{})

		if err := smm.Send(uint64(2), &TestEvent{A: "start"}); err != nil {
			t.Fatalf("%+v", err)
		}

		if err := smm.Stop(context.Background()); err != nil {
			t.Fatal(err)
			return
		}

		smm = New(ds, th, TestState{})
		if err := smm.Send(uint64(2), &TestEvent{A: "restart"}); err != nil {
			t.Fatalf("%+v", err)
		}
		close(th.proceed)

		<-th.done
	}
}

func TestPartialHandling(t *testing.T) {
	ds := datastore.NewMapDatastore()
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	th := &testHandlerPartial{
		t:        t,
		done:     make(chan struct{}),
		proceed:  make(chan struct{}),
		proceed2: make(chan struct{}),
		started:  make(chan struct{}),
		midpoint: make(chan struct{}),
	}
	smm := New(ds, th, TestState{})

	checkState := func(state uint64) {
		var testState TestState
		stored := smm.Get(uint64(2))
		err := stored.Get(&testState)
		assert.NilError(t, err)
		assert.Equal(t, testState.A, state)
	}

	// send first event
	if err := smm.Send(uint64(2), &TestEvent{A: "start"}); err != nil {
		t.Fatalf("%+v", err)
	}

	// wait for it to process
	select {
	case <-ctx.Done():
		t.Fatal("Did not process first event")
	case <-th.started:
	}

	// verify in first state
	checkState(1)

	// send two events with the first handler still running
	if err := smm.Send(uint64(2), &TestEvent{A: "b"}); err != nil {
		t.Fatalf("%+v", err)
	}

	if err := smm.Send(uint64(2), &TestEvent{A: "c"}); err != nil {
		t.Fatalf("%+v", err)
	}

	// complete first handler
	close(th.proceed)

	// wait for second event to process
	select {
	case <-ctx.Done():
		t.Fatal("Did not process second event")
	case <-th.midpoint:
	}

	// verify in second state
	checkState(2)

	// complete second handler
	close(th.proceed2)

	// wait for final event for to process -- assuming partial processing works right
	// plan should get called a third time and processed
	select {
	case <-ctx.Done():
		t.Fatal("Did not process third event")
	case <-th.done:
	}

	// verify in third state
	checkState(3)

}

type testHandlerPartial struct {
	t        *testing.T
	started  chan struct{}
	proceed  chan struct{}
	midpoint chan struct{}
	proceed2 chan struct{}
	done     chan struct{}
}

func (t *testHandlerPartial) Plan(events []Event, state interface{}) (interface{}, uint64, error) {
	return t.plan(events, state.(*TestState))
}

func (t *testHandlerPartial) plan(events []Event, state *TestState) (func(Context, TestState) error, uint64, error) {
	event := events[0]
	e := event.User.(*TestEvent)
	switch e.A {
	case "start":
		state.A = 1
	case "b":
		state.A = 2
	case "c":
		state.A = 3
	}

	switch state.A {
	case 1:
		return t.step0, 1, nil
	case 2:
		return t.step1, 1, nil
	case 3:
		return t.step2, 1, nil
	default:
		t.t.Fatal(state.A)
	}
	panic("how?")
}

func (t *testHandlerPartial) step0(ctx Context, st TestState) error {
	close(t.started)
	<-t.proceed
	return nil
}

func (t *testHandlerPartial) step1(ctx Context, st TestState) error {
	close(t.midpoint)
	<-t.proceed2
	return nil
}

func (t *testHandlerPartial) step2(ctx Context, st TestState) error {
	close(t.done)
	return nil
}

var _ StateHandler = &testHandler{}
var _ StateHandler = &testHandlerPartial{}
