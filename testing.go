package statemachine

type TestState struct {
	A uint64
	B uint64
	C []uint64
}

type TestEvent struct {
	A   string
	Val uint64
}
