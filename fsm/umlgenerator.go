package fsm

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
)

// StateNameMap maps a state type to a human readable string
type StateNameMap interface{}

// EventNameMap maps an event name to a human readable string
type EventNameMap interface{}

// SyntaxType specifies what kind of UML syntax we're generating
type SyntaxType uint64

const (
	PlantUML SyntaxType = iota
	MermaidUML
)

type eventDecl struct {
	start StateKey
	end   StateKey
	name  EventName
}

type anyEventDecl struct {
	end  StateKey
	name EventName
}

// GenerateUML genderates a UML state diagram (in Mermaid/PlantUML syntax) for a given FSM
func GenerateUML(w io.Writer, syntaxType SyntaxType, parameters Parameters, stateNameMap StateNameMap, eventNameMap EventNameMap, startStates []StateKey, includeFromAny bool, stateCmp func(a, b StateKey) bool) error {
	if err := VerifyStateParameters(parameters); err != nil {
		return err
	}
	if err := VerifyEventParameters(parameters.StateType, parameters.StateKeyField, parameters.Events); err != nil {
		return err
	}
	stateNameMapValue := reflect.ValueOf(stateNameMap)
	eventNameMapValue := reflect.ValueOf(eventNameMap)
	if err := checkNameMaps(parameters, stateNameMapValue, eventNameMapValue); err != nil {
		return err
	}
	if err := generateHeaderDeclaration(w, syntaxType); err != nil {
		return err
	}
	states := prepareStates(parameters.Events, stateCmp)
	for _, state := range states {
		if err := generateStateDeclaration(w, state, stateNameMapValue); err != nil {
			return err
		}
	}

	for _, state := range states {
		handler, ok := parameters.StateEntryFuncs[state]
		if ok {
			// warning: it is not a gaurantee that this will will work
			if err := generateStateDescription(w, state, handler); err != nil {
				return err
			}
		}
	}

	for _, state := range startStates {
		if err := generateStartStateDeclaration(w, state); err != nil {
			return err
		}
	}

	events, anyEvents, justRecordEvents := prepareEvents(parameters.Events, parameters.FinalityStates, states, includeFromAny)

	if err := generateFromAnyEventsDeclaration(w, anyEvents, states[0], stateNameMapValue, eventNameMapValue); err != nil {
		return err
	}

	for _, event := range events {
		if err := generateTransitionDeclaration(w, event.start, event.end, event.name, eventNameMapValue); err != nil {
			return err
		}
	}

	if err := generateJustRecordEventsDeclarations(w, states, justRecordEvents, stateNameMapValue, eventNameMapValue); err != nil {
		return err
	}

	for _, state := range parameters.FinalityStates {
		if err := generateEndStateDeclaration(w, state); err != nil {
			return err
		}
	}

	if err := generateFooterDeclaration(w, syntaxType); err != nil {
		return err
	}
	return nil
}

func generateHeaderDeclaration(w io.Writer, syntaxType SyntaxType) error {
	switch syntaxType {
	case PlantUML:
		_, err := fmt.Fprintf(w, "@startuml\n")
		return err
	case MermaidUML:
		_, err := fmt.Fprintf(w, "stateDiagram-v2\n")
		return err
	default:
		return errors.New("Unknown syntax format")
	}
}

func generateFooterDeclaration(w io.Writer, syntaxType SyntaxType) error {
	switch syntaxType {
	case PlantUML:
		_, err := fmt.Fprintf(w, "@enduml\n")
		return err
	case MermaidUML:
		return nil
	default:
		return errors.New("Unknown syntax format")
	}
}

func generateStateDeclaration(w io.Writer, state StateKey, stateNameMap reflect.Value) error {
	name := stateNameMap.MapIndex(reflect.ValueOf(state))
	_, err := fmt.Fprintf(w, "\tstate \"%s\" as %v\n", name.String(), state)
	return err
}

func generateStateDescription(w io.Writer, state StateKey, handler StateEntryFunc) error {
	handlerName := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
	handlerName = filepath.Ext(handlerName)
	handlerName = strings.TrimPrefix(handlerName, ".")
	_, err := fmt.Fprintf(w, "\t%v : On entry runs %s\n", state, handlerName)
	return err
}

func generateTransitionDeclaration(w io.Writer, startState StateKey, endState StateKey, eventName EventName, eventNameMap reflect.Value) error {
	name := eventNameMap.MapIndex(reflect.ValueOf(eventName))
	_, err := fmt.Fprintf(w, "\t%v --> %v : %s\n", startState, endState, name.String())
	return err
}

func generateFromAnyEventsDeclaration(w io.Writer, anyEvents []anyEventDecl, state StateKey, stateNameMap reflect.Value, eventNameMap reflect.Value) error {
	if len(anyEvents) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\tnote right of %v\n\t\tThe following events are not shown cause they can trigger from any state.\n\n", state); err != nil {
		return err
	}
	for _, anyEvent := range anyEvents {
		eventName := eventNameMap.MapIndex(reflect.ValueOf(anyEvent.name))
		if _, ok := anyEvent.end.(recordEvent); ok {
			if _, err := fmt.Fprintf(w, "\t\t%s - just records\n", eventName); err != nil {
				return err
			}
		} else if anyEvent.end == nil {
			if _, err := fmt.Fprintf(w, "\t\t%s - does not transition state\n", eventName); err != nil {
				return err
			}
		} else {
			stateName := stateNameMap.MapIndex(reflect.ValueOf(anyEvent.end))
			if _, err := fmt.Fprintf(w, "\t\t%s - transitions state to %s\n", eventName, stateName); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(w, "\tend note\n")
	return err
}

func generateJustRecordEventsDeclarations(w io.Writer, states []StateKey, justRecordEvents map[StateKey][]EventName, stateNameMap reflect.Value, eventNameMap reflect.Value) error {
	for _, state := range states {
		events, ok := justRecordEvents[state]
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(w, "\n\tnote left of %v : The following events only record in this state.<br>", state); err != nil {
			return err
		}
		for _, event := range events {
			if _, err := fmt.Fprintf(w, "<br>"); err != nil {
				return err
			}
			eventName := eventNameMap.MapIndex(reflect.ValueOf(event))
			if _, err := fmt.Fprintf(w, "%s", eventName); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "\n\n"); err != nil {
			return err
		}
	}
	return nil
}

func generateStartStateDeclaration(w io.Writer, state StateKey) error {
	_, err := fmt.Fprintf(w, "\t[*] --> %v\n", state)
	return err
}

func generateEndStateDeclaration(w io.Writer, state StateKey) error {
	_, err := fmt.Fprintf(w, "\t%v --> [*]\n", state)
	return err
}

func appendIfMissing(states []StateKey, state StateKey) []StateKey {
	for _, nextState := range states {
		if nextState == state {
			return states
		}
	}
	return append(states, state)
}

type sortableStates struct {
	keys     []StateKey
	stateCmp func(a, b StateKey) bool
}

func (s sortableStates) Len() int {
	return len(s.keys)
}
func (s sortableStates) Less(i, j int) bool { return s.stateCmp(s.keys[i], s.keys[j]) }
func (s sortableStates) Swap(i, j int)      { s.keys[i], s.keys[j] = s.keys[j], s.keys[i] }

func prepareStates(events []EventBuilder, stateCmp func(a, b StateKey) bool) []StateKey {
	var states []StateKey
	for _, evtIface := range events {
		evt := evtIface.(eventBuilder)
		for src, dst := range evt.transitionsSoFar {
			if src != nil {
				states = appendIfMissing(states, src)
			}
			_, justRecord := dst.(recordEvent)
			if dst != nil && !justRecord {
				states = appendIfMissing(states, dst)
			}
		}
	}
	sort.Sort(sortableStates{states, stateCmp})
	return states
}

func checkNameMaps(parameters Parameters, stateNameMapValue reflect.Value, eventNameMapValue reflect.Value) error {
	stateType := reflect.TypeOf(parameters.StateType)
	stateFieldType, _ := stateType.FieldByName(string(parameters.StateKeyField))
	if stateNameMapValue.Kind() != reflect.Map {
		return errors.New("stateNameMap must be a map")
	}
	if !stateNameMapValue.Type().Key().AssignableTo(stateFieldType.Type) {
		return errors.New("stateNameMap has wrong key type")
	}
	if stateNameMapValue.Type().Elem().Kind() != reflect.String {
		return errors.New("stateNameMap must have string values")
	}
	if eventNameMapValue.Kind() != reflect.Map {
		return errors.New("eventNameMap must be a map")
	}
	if eventNameMapValue.Type().Elem().Kind() != reflect.String {
		return errors.New("eventNameMap must have string values")
	}
	return nil
}

func getNonFinalityStates(states []StateKey, finalityStates []StateKey) []StateKey {
	var nonFinalityStates []StateKey
	for _, state := range states {
		isFinality := false
		for _, finalityState := range finalityStates {
			if state == finalityState {
				isFinality = true
				break
			}
		}
		if !isFinality {
			nonFinalityStates = append(nonFinalityStates, state)
		}
	}
	return nonFinalityStates
}

func prepareEvents(srcEvents []EventBuilder, finalityStates []StateKey, states []StateKey, includeFromAny bool) ([]eventDecl, []anyEventDecl, map[StateKey][]EventName) {
	var events []eventDecl
	var anyEvents []anyEventDecl
	justRecordEvents := make(map[StateKey][]EventName)
	nonFinalityStates := getNonFinalityStates(states, finalityStates)
	for _, evtIface := range srcEvents {
		evt := evtIface.(eventBuilder)
		dst, ok := evt.transitionsSoFar[nil]
		if ok {
			if includeFromAny {
				for _, state := range nonFinalityStates {
					if _, ok := dst.(recordEvent); ok {
						justRecordEvents[state] = append(justRecordEvents[state], evt.name)
					} else if dst == nil {
						events = append(events, eventDecl{state, state, evt.name})
					} else {
						events = append(events, eventDecl{state, dst, evt.name})
					}
				}
			} else {
				anyEvents = append(anyEvents, anyEventDecl{dst, evt.name})
			}
		}
		for _, src := range states {
			dst, ok := evt.transitionsSoFar[src]
			if ok {
				if _, ok := dst.(recordEvent); ok {
					justRecordEvents[src] = append(justRecordEvents[src], evt.name)
				} else if dst == nil {
					events = append(events, eventDecl{src, src, evt.name})
				} else {
					events = append(events, eventDecl{src, dst, evt.name})
				}
			}
		}
	}
	return events, anyEvents, justRecordEvents
}
