package fsm_test

import (
	"context"
	"testing"

	"github.com/ipfs/go-datastore"
	logging "github.com/ipfs/go-log"
	"github.com/stretchr/testify/require"
	"gotest.tools/assert"

	"github.com/filecoin-project/go-statemachine"
	"github.com/filecoin-project/go-statemachine/fsm"
)

func init() {
	logging.SetLogLevel("*", "INFO") // nolint: errcheck
}

type testWorld struct {
	universalCalls uint64
	t              *testing.T
	proceed        chan struct{}
	done           chan struct{}
}

var events = fsm.Events{
	"start": {
		TransitionMap: fsm.TransitionMap{
			uint64(0): uint64(1),
		},
	},
	"restart": {
		TransitionMap: fsm.TransitionMap{
			uint64(1): uint64(1),
			uint64(2): uint64(1),
		},
	},
	"b": {
		TransitionMap: fsm.TransitionMap{
			uint64(1): uint64(2),
		},
		ApplyTransition: func(state *statemachine.TestState, val uint64) error {
			state.B = val
			return nil
		},
	},
	"resume": {
		TransitionMap: fsm.TransitionMap{
			uint64(1): nil,
			uint64(2): nil,
		},
	},
	"any": {
		TransitionMap: fsm.TransitionMap{
			nil: uint64(1),
		},
	},
}

var stateHandlers = fsm.StateHandlers{

	uint64(1): func(ctx fsm.Context, tw *testWorld, ts statemachine.TestState) error {
		err := ctx.Event("b", uint64(55))
		assert.NilError(tw.t, err)
		<-tw.proceed
		return nil
	},
	uint64(2): func(ctx fsm.Context, tw *testWorld, ts statemachine.TestState) error {

		assert.Equal(tw.t, uint64(2), ts.A)
		close(tw.done)
		return nil
	},
}

func TestTypeCheckingOnSetup(t *testing.T) {
	ds := datastore.NewMapDatastore()
	tw := &testWorld{t: t, done: make(chan struct{}), proceed: make(chan struct{})}
	t.Run("Bad state field", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			WorldBuilder: func(id fsm.Identifier) *testWorld {
				return tw
			},
			StateType:     statemachine.TestState{},
			StateKeyField: "Jesus",
			Events:        events,
			StateHandlers: stateHandlers,
			Notifier:      nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "state type has no field `Jesus`")
	})
	t.Run("State field not comparable", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			WorldBuilder: func(id fsm.Identifier) *testWorld {
				return tw
			},
			StateType:     statemachine.TestState{},
			StateKeyField: "C",
			Events:        events,
			StateHandlers: stateHandlers,
			Notifier:      nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "state field `C` is not comparable")
	})
	t.Run("Event description has bad source type", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			WorldBuilder: func(id fsm.Identifier) *testWorld {
				return tw
			},
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events: fsm.Events{
				"start": {
					TransitionMap: fsm.TransitionMap{
						"happy": uint64(1),
					},
				},
			},
			StateHandlers: stateHandlers,
			Notifier:      nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "event `start` source type is not assignable to: uint64")
	})
	t.Run("Event description has bad destination type", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			WorldBuilder: func(id fsm.Identifier) *testWorld {
				return tw
			},
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events: fsm.Events{
				"start": {
					TransitionMap: fsm.TransitionMap{
						uint64(1): "happy",
					},
				},
			},
			StateHandlers: stateHandlers,
			Notifier:      nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "event `start` destination type is not assignable to: uint64")
	})
	t.Run("Event description has callback that is not a function", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			WorldBuilder: func(id fsm.Identifier) *testWorld {
				return tw
			},
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events: fsm.Events{
				"b": {
					TransitionMap: fsm.TransitionMap{
						uint64(1): uint64(2),
					},
					ApplyTransition: "applesuace",
				},
			},
			StateHandlers: stateHandlers,
			Notifier:      nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "event `b` has a callback that is not a function")
	})
	t.Run("Event description has callback with no parameters", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			WorldBuilder: func(id fsm.Identifier) *testWorld {
				return tw
			},
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events: fsm.Events{
				"b": {
					TransitionMap: fsm.TransitionMap{
						uint64(1): uint64(2),
					},
					ApplyTransition: func() {},
				},
			},
			StateHandlers: stateHandlers,
			Notifier:      nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "event `b` has a callback that does not take the state")
	})
	t.Run("Event description has callback with wrong first parameter", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			WorldBuilder: func(id fsm.Identifier) *testWorld {
				return tw
			},
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events: fsm.Events{
				"b": {
					TransitionMap: fsm.TransitionMap{
						uint64(1): uint64(2),
					},
					ApplyTransition: func(uint64) error { return nil },
				},
			},
			StateHandlers: stateHandlers,
			Notifier:      nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "event `b` has a callback that does not take the state")
	})
	t.Run("Event description has callback that doesn't return an error", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			WorldBuilder: func(id fsm.Identifier) *testWorld {
				return tw
			},
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events: fsm.Events{
				"b": {
					TransitionMap: fsm.TransitionMap{
						uint64(1): uint64(2),
					},
					ApplyTransition: func(*statemachine.TestState) {},
				},
			},
			StateHandlers: stateHandlers,
			Notifier:      nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "event `b` callback should return exactly one param that is an error")
	})
	t.Run("State Handler with bad stateKey", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			WorldBuilder: func(id fsm.Identifier) *testWorld {
				return tw
			},
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events:        events,
			StateHandlers: fsm.StateHandlers{
				"apples": func(ctx fsm.Context, tw *testWorld, ts statemachine.TestState) error {
					err := ctx.Event("b", uint64(55))
					assert.NilError(tw.t, err)
					<-tw.proceed
					return nil
				},
			},
			Notifier: nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "state key is not assignable to: uint64")
	})
	t.Run("State Handler with bad statehandler", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			WorldBuilder: func(id fsm.Identifier) *testWorld {
				return tw
			},
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events:        events,
			StateHandlers: fsm.StateHandlers{
				uint64(1): func(ctx fsm.Context, tw *testWorld, u uint64) error {
					return nil
				},
			},
			Notifier: nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "handler for state does not match expected type")
	})
}

func newFsm(ds datastore.Datastore, tw *testWorld) (fsm.Group, error) {
	defaultFsmParams := fsm.Parameters{
		WorldBuilder: func(id fsm.Identifier) *testWorld {
			return tw
		},
		StateType:     statemachine.TestState{},
		StateKeyField: "A",
		Events:        events,
		StateHandlers: stateHandlers,
		Notifier:      nil,
	}
	return fsm.New(ds, defaultFsmParams)
}

func TestArgumentChecks(t *testing.T) {
	ds := datastore.NewMapDatastore()
	tw := &testWorld{t: t, done: make(chan struct{}), proceed: make(chan struct{})}
	smm, err := newFsm(ds, tw)
	close(tw.proceed)
	require.NoError(t, err)

	// should take B with correct arguments
	err = smm.Send(uint64(2), "b", uint64(55))
	require.NoError(t, err)

	// should not take b with incorrect argument count
	err = smm.Send(uint64(2), "b", uint64(55), "applesuace")
	require.Regexp(t, "^Wrong number of arguments for event `b`", err.Error())

	// should not take b with incorrect argument type
	err = smm.Send(uint64(2), "b", "applesuace")
	require.Regexp(t, "^Incorrect argument type at index `0`", err.Error())

}

func TestBasic(t *testing.T) {
	for i := 0; i < 1000; i++ { // run a few times to expose any races
		ds := datastore.NewMapDatastore()

		tw := &testWorld{t: t, done: make(chan struct{}), proceed: make(chan struct{})}
		close(tw.proceed)
		smm, err := newFsm(ds, tw)
		require.NoError(t, err)

		err = smm.Send(uint64(2), "start")
		require.NoError(t, err)

		<-tw.done

	}
}

func TestPersist(t *testing.T) {
	for i := 0; i < 1000; i++ { // run a few times to expose any races
		ds := datastore.NewMapDatastore()

		tw := &testWorld{t: t, done: make(chan struct{}), proceed: make(chan struct{})}
		smm, err := newFsm(ds, tw)
		require.NoError(t, err)

		err = smm.Send(uint64(2), "start")
		require.NoError(t, err)

		if err := smm.Stop(context.Background()); err != nil {
			t.Fatal(err)
			return
		}

		smm, err = newFsm(ds, tw)
		require.NoError(t, err)
		err = smm.Send(uint64(2), "restart")
		require.NoError(t, err)

		close(tw.proceed)

		<-tw.done
	}
}

func TestSyncEventHandling(t *testing.T) {
	ctx := context.Background()
	ds := datastore.NewMapDatastore()

	tw := &testWorld{t: t, done: make(chan struct{}), proceed: make(chan struct{})}
	smm, err := newFsm(ds, tw)
	close(tw.proceed)
	require.NoError(t, err)

	// events that should fail based on state, only picked up with SendSync

	err = smm.Send(uint64(2), "b", uint64(55))
	require.NoError(t, err)

	err = smm.SendSync(ctx, uint64(2), "b", uint64(55))
	require.Error(t, err)
	require.EqualError(t, err, "Invalid transition in queue, state `0`, event `b`")

	err = smm.Send(uint64(2), "restart")
	require.NoError(t, err)

	err = smm.SendSync(ctx, uint64(2), "restart")
	require.Error(t, err)
	require.EqualError(t, err, "Invalid transition in queue, state `0`, event `restart`")

}

func TestNotification(t *testing.T) {
	notifications := 0

	var notifier fsm.Notifier = func(eventName fsm.EventName, state fsm.StateType) {
		notifications++
	}

	ds := datastore.NewMapDatastore()

	tw := &testWorld{t: t, done: make(chan struct{}), proceed: make(chan struct{}), universalCalls: 0}
	close(tw.proceed)
	params := fsm.Parameters{
		WorldBuilder: func(id fsm.Identifier) *testWorld {
			return tw
		},
		StateType:     statemachine.TestState{},
		StateKeyField: "A",
		Events:        events,
		StateHandlers: stateHandlers,
		Notifier:      notifier,
	}
	smm, err := fsm.New(ds, params)
	require.NoError(t, err)

	err = smm.Send(uint64(2), "start")
	require.NoError(t, err)
	<-tw.done

	require.Equal(t, notifications, 2)
}

func TestNoChangeHandler(t *testing.T) {
	ds := datastore.NewMapDatastore()

	tw := &testWorld{t: t, done: make(chan struct{}), proceed: make(chan struct{}), universalCalls: 0}
	close(tw.proceed)
	smm, err := newFsm(ds, tw)
	require.NoError(t, err)

	err = smm.Send(uint64(2), "start")
	require.NoError(t, err)
	<-tw.done

	tw.done = make(chan struct{})
	// call resume to retrigger step2
	err = smm.Send(uint64(2), "resume")
	require.NoError(t, err)
	<-tw.done
}

func TestAllStateEvent(t *testing.T) {
	ds := datastore.NewMapDatastore()

	tw := &testWorld{t: t, done: make(chan struct{}), proceed: make(chan struct{}), universalCalls: 0}
	close(tw.proceed)
	smm, err := newFsm(ds, tw)
	require.NoError(t, err)

	// any can run from any state and function like start
	err = smm.Send(uint64(2), "any")
	require.NoError(t, err)
	<-tw.done

	tw.done = make(chan struct{})
	// here any can function like a restart handler
	err = smm.Send(uint64(2), "any")
	require.NoError(t, err)
	<-tw.done
}
