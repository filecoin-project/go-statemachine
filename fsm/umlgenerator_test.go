package fsm_test

import (
	"bytes"
	"testing"

	"github.com/filecoin-project/go-statemachine"
	"github.com/filecoin-project/go-statemachine/fsm"
	"github.com/stretchr/testify/require"
)

var stateNameMap = map[uint64]string{
	uint64(0): "Start State",
	uint64(1): "State A",
	uint64(2): "State B",
	uint64(3): "Final State",
}

var eventNameMap = map[string]string{
	"start":   "Start!",
	"restart": "Restart!",
	"b":       "B!",
	"resume":  "Resume!",
	"any":     "Any!",
	"finish":  "Finish!",
}

var expectedString = `stateDiagram-v2
	state "Start State" as 0
	state "State A" as 1
	state "State B" as 2
	state "Final State" as 3
	[*] --> 0
	0 --> 1 : Start!
	1 --> 1 : Restart!
	2 --> 1 : Restart!
	1 --> 2 : B!
	1 --> 1 : Resume!
	2 --> 2 : Resume!
	0 --> 1 : Any!
	1 --> 1 : Any!
	2 --> 1 : Any!
	0 --> 3 : Finish!
	1 --> 3 : Finish!
	2 --> 3 : Finish!
	3 --> [*]
`

func TestGenerateUML(t *testing.T) {
	defaultFsmParams := fsm.Parameters{
		Environment:     &testEnvironment{},
		StateType:       statemachine.TestState{},
		StateKeyField:   "A",
		Events:          events,
		StateEntryFuncs: stateEntryFuncs,
		FinalityStates:  []fsm.StateKey{uint64(3)},
		Notifier:        nil,
	}
	buf := new(bytes.Buffer)
	err := fsm.GenerateUML(buf, fsm.MermaidUML, defaultFsmParams, stateNameMap, eventNameMap, []fsm.StateKey{uint64(0)}, true)
	require.NoError(t, err)
	require.Equal(t, expectedString, string(buf.Bytes()))
}
