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

type testEnvironment struct {
	universalCalls uint64
	t              *testing.T
	proceed        chan struct{}
	done           chan struct{}
}

var events = fsm.Events{
	fsm.Event("start").From(uint64(0)).To(uint64(1)),
	fsm.Event("restart").FromMany(uint64(1), uint64(2)).To(uint64(1)),
	fsm.Event("b").From(uint64(1)).To(uint64(2)).Action(
		func(state *statemachine.TestState, val uint64) error {
			state.B = val
			return nil
		},
	),
	fsm.Event("resume").FromMany(uint64(1), uint64(2)).ToNoChange(),
	fsm.Event("any").FromAny().To(uint64(1)),
}

var stateEntryFuncs = fsm.StateEntryFuncs{

	uint64(1): func(ctx fsm.Context, te *testEnvironment, ts statemachine.TestState) error {
		err := ctx.Trigger("b", uint64(55))
		assert.NilError(te.t, err)
		<-te.proceed
		return nil
	},
	uint64(2): func(ctx fsm.Context, te *testEnvironment, ts statemachine.TestState) error {

		assert.Equal(te.t, uint64(2), ts.A)
		close(te.done)
		return nil
	},
}

func TestTypeCheckingOnSetup(t *testing.T) {
	ds := datastore.NewMapDatastore()
	te := &testEnvironment{t: t, done: make(chan struct{}), proceed: make(chan struct{})}
	t.Run("Bad state field", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:     te,
			StateType:       statemachine.TestState{},
			StateKeyField:   "Jesus",
			Events:          events,
			StateEntryFuncs: stateEntryFuncs,
			Notifier:        nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "state type has no field `Jesus`")
	})
	t.Run("State field not comparable", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:     te,
			StateType:       statemachine.TestState{},
			StateKeyField:   "C",
			Events:          events,
			StateEntryFuncs: stateEntryFuncs,
			Notifier:        nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "state field `C` is not comparable")
	})
	t.Run("Event description has bad source type", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment: te,

			StateType:       statemachine.TestState{},
			StateKeyField:   "A",
			Events:          fsm.Events{fsm.Event("start").From("happy").To(uint64(1))},
			StateEntryFuncs: stateEntryFuncs,
			Notifier:        nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "event `start` source type is not assignable to: uint64")
	})
	t.Run("Event description has bad destination type", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment: te,

			StateType:       statemachine.TestState{},
			StateKeyField:   "A",
			Events:          fsm.Events{fsm.Event("start").From(uint64(1)).To("happy")},
			StateEntryFuncs: stateEntryFuncs,
			Notifier:        nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "event `start` destination type is not assignable to: uint64")
	})
	t.Run("Event description has callback that is not a function", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:     te,
			StateType:       statemachine.TestState{},
			StateKeyField:   "A",
			Events:          fsm.Events{fsm.Event("b").From(uint64(1)).To(uint64(2)).Action("applesuace")},
			StateEntryFuncs: stateEntryFuncs,
			Notifier:        nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "event `b` has a callback that is not a function")
	})
	t.Run("Event description has callback with no parameters", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:     te,
			StateType:       statemachine.TestState{},
			StateKeyField:   "A",
			Events:          fsm.Events{fsm.Event("b").From(uint64(1)).To(uint64(2)).Action(func() {})},
			StateEntryFuncs: stateEntryFuncs,
			Notifier:        nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "event `b` has a callback that does not take the state")
	})
	t.Run("Event description has callback with wrong first parameter", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:   te,
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events: fsm.Events{
				fsm.Event("b").From(uint64(1)).To(uint64(2)).Action(func(uint64) error { return nil }),
			},
			StateEntryFuncs: stateEntryFuncs,
			Notifier:        nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "event `b` has a callback that does not take the state")
	})
	t.Run("Event description has callback that doesn't return an error", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:   te,
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events: fsm.Events{
				fsm.Event("b").From(uint64(1)).To(uint64(2)).Action(func(*statemachine.TestState) {}),
			},
			StateEntryFuncs: stateEntryFuncs,
			Notifier:        nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "event `b` callback should return exactly one param that is an error")
	})
	t.Run("Event description has transition source twice", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:   te,
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events: fsm.Events{
				fsm.Event("b").From(uint64(1)).To(uint64(2)).From(uint64(1)).To(uint64(0)),
			},
			StateEntryFuncs: stateEntryFuncs,
			Notifier:        nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "duplicate transition source `1` for event `b`")
	})
	t.Run("Event description has overlapping transition source twice", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:   te,
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events: fsm.Events{
				fsm.Event("b").FromMany(uint64(0), uint64(1)).To(uint64(2)).FromMany(uint64(2), uint64(1)).To(uint64(0)),
			},
			StateEntryFuncs: stateEntryFuncs,
			Notifier:        nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "duplicate transition source `1` for event `b`")
	})
	t.Run("Event description has from any source twice", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:   te,
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events: fsm.Events{
				fsm.Event("b").FromAny().To(uint64(2)).FromAny().To(uint64(0)),
			},
			StateEntryFuncs: stateEntryFuncs,
			Notifier:        nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "duplicate all-sources destination for event `b`")
	})
	t.Run("Event description has callback defined twice", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:   te,
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events: fsm.Events{
				fsm.Event("b").From(uint64(1)).To(uint64(2)).Action(func(*statemachine.TestState) error {
					return nil
				}).Action(func(*statemachine.TestState) error {
					return nil
				}),
			},
			StateEntryFuncs: stateEntryFuncs,
			Notifier:        nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "duplicate action for event `b`")
	})
	t.Run("State Handler with bad stateKey", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:   te,
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events:        events,
			StateEntryFuncs: fsm.StateEntryFuncs{
				"apples": func(ctx fsm.Context, te *testEnvironment, ts statemachine.TestState) error {
					err := ctx.Trigger("b", uint64(55))
					assert.NilError(te.t, err)
					<-te.proceed
					return nil
				},
			},
			Notifier: nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "state key is not assignable to: uint64")
	})
	t.Run("State Handler is not a function", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:   te,
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events:        events,
			StateEntryFuncs: fsm.StateEntryFuncs{
				uint64(1): "cheese",
			},
			Notifier: nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "handler for state is not a function")
	})
	t.Run("State Handler has wrong parameter count", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:   te,
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events:        events,
			StateEntryFuncs: fsm.StateEntryFuncs{
				uint64(1): func() error {
					return nil
				},
			},
			Notifier: nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "handler for state does not take correct number of arguments")
	})
	t.Run("State Handler has no context parameter", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:   te,
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events:        events,
			StateEntryFuncs: fsm.StateEntryFuncs{
				uint64(1): func(ctx uint64, te *testEnvironment, ts statemachine.TestState) error {
					return nil
				},
			},
			Notifier: nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "handler for state does not match context parameter")
	})
	t.Run("State Handler has wrong environment parameter", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:   te,
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events:        events,
			StateEntryFuncs: fsm.StateEntryFuncs{
				uint64(1): func(ctx fsm.Context, te uint64, ts statemachine.TestState) error {
					return nil
				},
			},
			Notifier: nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "handler for state does not match environment parameter")
	})
	t.Run("State Handler has wrong state parameter", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:   te,
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events:        events,
			StateEntryFuncs: fsm.StateEntryFuncs{
				uint64(1): func(ctx fsm.Context, te *testEnvironment, ts uint64) error {
					return nil
				},
			},
			Notifier: nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "handler for state does not match state parameter")
	})

	t.Run("State Handler has wrong return", func(t *testing.T) {
		smm, err := fsm.New(ds, fsm.Parameters{
			Environment:   te,
			StateType:     statemachine.TestState{},
			StateKeyField: "A",
			Events:        events,
			StateEntryFuncs: fsm.StateEntryFuncs{
				uint64(1): func(ctx fsm.Context, te *testEnvironment, ts statemachine.TestState) {
				},
			},
			Notifier: nil,
		})
		require.Nil(t, smm)
		require.EqualError(t, err, "handler for state does not return an error")
	})
}

func newFsm(ds datastore.Datastore, te *testEnvironment) (fsm.Group, error) {
	defaultFsmParams := fsm.Parameters{
		Environment:     te,
		StateType:       statemachine.TestState{},
		StateKeyField:   "A",
		Events:          events,
		StateEntryFuncs: stateEntryFuncs,
		Notifier:        nil,
	}
	return fsm.New(ds, defaultFsmParams)
}

func TestArgumentChecks(t *testing.T) {
	ds := datastore.NewMapDatastore()
	te := &testEnvironment{t: t, done: make(chan struct{}), proceed: make(chan struct{})}
	smm, err := newFsm(ds, te)
	close(te.proceed)
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

		te := &testEnvironment{t: t, done: make(chan struct{}), proceed: make(chan struct{})}
		close(te.proceed)
		smm, err := newFsm(ds, te)
		require.NoError(t, err)

		err = smm.Send(uint64(2), "start")
		require.NoError(t, err)

		<-te.done

	}
}

func TestPersist(t *testing.T) {
	for i := 0; i < 1000; i++ { // run a few times to expose any races
		ds := datastore.NewMapDatastore()

		te := &testEnvironment{t: t, done: make(chan struct{}), proceed: make(chan struct{})}
		smm, err := newFsm(ds, te)
		require.NoError(t, err)

		err = smm.Send(uint64(2), "start")
		require.NoError(t, err)

		if err := smm.Stop(context.Background()); err != nil {
			t.Fatal(err)
			return
		}

		smm, err = newFsm(ds, te)
		require.NoError(t, err)
		err = smm.Send(uint64(2), "restart")
		require.NoError(t, err)

		close(te.proceed)

		<-te.done
	}
}

func TestSyncEventHandling(t *testing.T) {
	ctx := context.Background()
	ds := datastore.NewMapDatastore()

	te := &testEnvironment{t: t, done: make(chan struct{}), proceed: make(chan struct{})}
	smm, err := newFsm(ds, te)
	close(te.proceed)
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

	te := &testEnvironment{t: t, done: make(chan struct{}), proceed: make(chan struct{}), universalCalls: 0}
	close(te.proceed)
	params := fsm.Parameters{
		Environment:     te,
		StateType:       statemachine.TestState{},
		StateKeyField:   "A",
		Events:          events,
		StateEntryFuncs: stateEntryFuncs,
		Notifier:        notifier,
	}
	smm, err := fsm.New(ds, params)
	require.NoError(t, err)

	err = smm.Send(uint64(2), "start")
	require.NoError(t, err)
	<-te.done

	require.Equal(t, notifications, 2)
}

func TestNoChangeHandler(t *testing.T) {
	ds := datastore.NewMapDatastore()

	te := &testEnvironment{t: t, done: make(chan struct{}), proceed: make(chan struct{}), universalCalls: 0}
	close(te.proceed)
	smm, err := newFsm(ds, te)
	require.NoError(t, err)

	err = smm.Send(uint64(2), "start")
	require.NoError(t, err)
	<-te.done

	te.done = make(chan struct{})
	// call resume to retrigger step2
	err = smm.Send(uint64(2), "resume")
	require.NoError(t, err)
	<-te.done
}

func TestAllStateEvent(t *testing.T) {
	ds := datastore.NewMapDatastore()

	te := &testEnvironment{t: t, done: make(chan struct{}), proceed: make(chan struct{}), universalCalls: 0}
	close(te.proceed)
	smm, err := newFsm(ds, te)
	require.NoError(t, err)

	// any can run from any state and function like start
	err = smm.Send(uint64(2), "any")
	require.NoError(t, err)
	<-te.done

	te.done = make(chan struct{})
	// here any can function like a restart handler
	err = smm.Send(uint64(2), "any")
	require.NoError(t, err)
	<-te.done
}
