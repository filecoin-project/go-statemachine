package statemachine

import (
	"context"
	"testing"
	"time"

	"github.com/ipfs/go-datastore"
	logging "github.com/ipfs/go-log/v2"
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

func TestBegin(t *testing.T) {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ds := datastore.NewMapDatastore()

	th := &testHandler{t: t, done: make(chan struct{}), proceed: make(chan struct{})}
	smm := New(ds, th, TestState{})

	// should not work for an invalid starting value
	err := smm.Begin(uint64(2), uint64(4))
	assert.Error(t, err, "loadOrCreate state: initialized item with incorrect type uint64")

	// should initiate tracking successfully
	err = smm.Begin(uint64(2), &TestState{A: 2})
	assert.NilError(t, err)

	// assert initialized with non default value
	storedVal := smm.Get(uint64(2))
	var ts TestState
	err = storedVal.Get(&ts)
	assert.NilError(t, err)
	assert.Equal(t, ts.A, uint64(2))

	// assert cannot initialize twice
	err = smm.Begin(uint64(2), &TestState{A: 2})
	assert.Error(t, err, "Begin: already tracking identifier `2`")

	smm.Stop(ctx)

	// assert cannot initialize even if just stored twice
	smm = New(ds, th, TestState{})
	err = smm.Begin(uint64(2), &TestState{A: 2})
	assert.Error(t, err, "Begin: cannot initiate a state for identifier `2` that already exists")

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

func TestNoStateCallback(t *testing.T) {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ds := datastore.NewMapDatastore()

	th := &testHandlerNoStateCB{t: t, proceed: make(chan struct{}), done: make(chan struct{})}
	smm := New(ds, th, TestState{})

	if err := smm.Send(uint64(2), &TestEvent{A: "block"}); err != nil {
		t.Fatalf("%+v", err)
	}

	if err := smm.Send(uint64(2), &TestEvent{A: "start"}); err != nil {
		t.Fatalf("%+v", err)
	}

	if err := smm.Send(uint64(2), &TestEvent{A: "b", Val: 55}); err != nil {
		t.Fatalf("%+v", err)
	}

	close(th.proceed)
	select {
	case <-ctx.Done():
		t.Fatal("Second event transition not processed")
	case <-th.done:
	}
}

type testHandlerNoStateCB struct {
	t       *testing.T
	proceed chan struct{}
	done    chan struct{}
}

func (t *testHandlerNoStateCB) Plan(events []Event, state interface{}) (interface{}, uint64, error) {
	cb, processed, err := t.plan(events, state.(*TestState))
	if cb == nil {
		return nil, processed, err
	}
	return cb, processed, err
}

func (t *testHandlerNoStateCB) plan(events []Event, state *TestState) (func(Context, TestState) error, uint64, error) {
	e := events[0].User.(*TestEvent)
	switch e.A {
	case "restart":
	case "start":
		state.A = 1
	case "b":
		state.A = 2
		state.B = e.Val
	case "block":
		state.A = 3
	}

	switch state.A {
	case 1:
		return nil, uint64(1), nil
	case 2:
		return t.step1, uint64(1), nil
	case 3:
		return t.block, uint64(1), nil
	default:
		t.t.Fatal(state.A)
	}
	panic("how?")
}

func (t *testHandlerNoStateCB) block(ctx Context, st TestState) error {
	<-t.proceed
	return nil
}

func (t *testHandlerNoStateCB) step1(ctx Context, st TestState) error {
	assert.Equal(t.t, uint64(2), st.A)

	close(t.done)
	return nil
}

type testHandlerWithGoRoutine struct {
	t         *testing.T
	event     chan struct{}
	proceed   chan struct{}
	done      chan struct{}
	notifDone chan struct{}
	count     uint64
}

func (t *testHandlerWithGoRoutine) Plan(events []Event, state interface{}) (interface{}, uint64, error) {
	return t.plan(events, state.(*TestState))
}

func (t *testHandlerWithGoRoutine) Init(onClose <-chan struct{}) {
	go func() {
		for {
			select {
			case <-t.event:
				t.count++
			case <-onClose:
				close(t.notifDone)
				return
			}
		}
	}()
}

func (t *testHandlerWithGoRoutine) plan(events []Event, state *TestState) (func(Context, TestState) error, uint64, error) {
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

	t.event <- struct{}{}
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

func (t *testHandlerWithGoRoutine) step0(ctx Context, st TestState) error {
	ctx.Send(&TestEvent{A: "b", Val: 55}) // nolint: errcheck
	<-t.proceed
	return nil
}

func (t *testHandlerWithGoRoutine) step1(ctx Context, st TestState) error {
	assert.Equal(t.t, uint64(2), st.A)

	close(t.done)
	return nil
}

func TestInit(t *testing.T) {
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for i := 0; i < 1000; i++ { // run a few times to expose any races
		ds := datastore.NewMapDatastore()

		th := &testHandlerWithGoRoutine{t: t, event: make(chan struct{}), notifDone: make(chan struct{}), done: make(chan struct{}), proceed: make(chan struct{})}
		close(th.proceed)
		smm := New(ds, th, TestState{})

		if err := smm.Send(uint64(2), &TestEvent{A: "start"}); err != nil {
			t.Fatalf("%+v", err)
		}

		<-th.done
		err := smm.Stop(ctx)
		assert.NilError(t, err)
		<-th.notifDone
		assert.Equal(t, uint64(2), th.count)
	}

}

var _ StateHandler = &testHandler{}
var _ StateHandler = &testHandlerPartial{}
var _ StateHandler = &testHandlerNoStateCB{}
var _ StateHandler = &testHandlerWithGoRoutine{}
