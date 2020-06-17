package fsm

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"runtime"
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

// GenerateUML genderates a UML state diagram (in Mermaid/PlantUML syntax) for a given FSM
func GenerateUML(w io.Writer, syntaxType SyntaxType, parameters Parameters, stateNameMap StateNameMap, eventNameMap EventNameMap, startStates []StateKey, includeFromAny bool) error {
	err := VerifyStateParameters(parameters)
	if err != nil {
		return err
	}
	err = VerifyEventParameters(parameters.StateType, parameters.StateKeyField, parameters.Events)
	if err != nil {
		return err
	}
	stateType := reflect.TypeOf(parameters.StateType)
	stateFieldType, _ := stateType.FieldByName(string(parameters.StateKeyField))
	stateNameMapValue := reflect.ValueOf(stateNameMap)
	if stateNameMapValue.Kind() != reflect.Map {
		return errors.New("stateNameMap must be a map")
	}
	if !stateNameMapValue.Type().Key().AssignableTo(stateFieldType.Type) {
		return errors.New("stateNameMap has wrong key type")
	}
	if stateNameMapValue.Type().Elem().Kind() != reflect.String {
		return errors.New("stateNameMap must have string values")
	}
	eventNameMapValue := reflect.ValueOf(eventNameMap)
	if eventNameMapValue.Kind() != reflect.Map {
		return errors.New("eventNameMap must be a map")
	}
	if eventNameMapValue.Type().Elem().Kind() != reflect.String {
		return errors.New("eventNameMap must have string values")
	}
	if err := generateHeaderDeclaration(w, syntaxType); err != nil {
		return err
	}
	var states []StateKey
	for _, evtIface := range parameters.Events {
		evt := evtIface.(eventBuilder)
		for src, dst := range evt.transitionsSoFar {
			if src != nil {
				states = appendIfMissing(states, src)
			}
			if dst != nil {
				states = appendIfMissing(states, dst)
			}
		}
	}
	for _, state := range states {
		if err := generateStateDeclaration(w, state, stateNameMapValue); err != nil {
			return err
		}
	}

	for _, state := range states {
		handler, ok := parameters.StateEntryFuncs[state]
		if ok {
			// warning: it is not a gaurantee that this will will work
			handlerName := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
			handlerName = filepath.Ext(handlerName)
			handlerName = strings.TrimPrefix(handlerName, ".")
			generateStateDescription(w, state, handlerName)
		}
	}

	for _, state := range startStates {
		if err := generateStartStateDeclaration(w, state); err != nil {
			return err
		}
	}
	var nonFinalityStates []StateKey
	for _, state := range states {
		isFinality := false
		for _, finalityState := range parameters.FinalityStates {
			if state == finalityState {
				isFinality = true
				break
			}
		}
		if !isFinality {
			nonFinalityStates = append(nonFinalityStates, state)
		}
	}
	var events []eventDecl
	for _, evtIface := range parameters.Events {
		evt := evtIface.(eventBuilder)
		for src, dst := range evt.transitionsSoFar {
			if src == nil {
				if includeFromAny {
					for _, state := range nonFinalityStates {
						if dst == nil {
							events = append(events, eventDecl{state, state, evt.name})
						} else {
							events = append(events, eventDecl{state, dst, evt.name})
						}
					}
				}
			} else {
				if dst == nil {
					events = append(events, eventDecl{src, src, evt.name})
				} else {
					events = append(events, eventDecl{src, dst, evt.name})
				}
			}
		}
	}

	for _, event := range events {
		if err := generateTransitionDeclaration(w, event.start, event.end, event.name, eventNameMapValue); err != nil {
			return err
		}
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

func generateStateDescription(w io.Writer, state StateKey, handlerName string) error {
	_, err := fmt.Fprintf(w, "\t%v : On entry runs %s\n", state, handlerName)
	return err
}

func generateTransitionDeclaration(w io.Writer, startState StateKey, endState StateKey, eventName EventName, eventNameMap reflect.Value) error {
	name := eventNameMap.MapIndex(reflect.ValueOf(eventName))
	_, err := fmt.Fprintf(w, "\t%v --> %v : %s\n", startState, endState, name.String())
	return err
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
