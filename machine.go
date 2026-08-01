package fsm

import (
	"context"
	"fmt"
)

// Transition describes one move: where it started, where it leads, and the event that caused it.
//
// It is the value handed to guards and hooks, so they can act on the whole move rather than only on the state they were
// registered against.
//
// Example:
//
//	func(ctx context.Context, t fsm.Transition[OrderState, OrderEvent]) error {
//		log.Info("moving", "from", t.From, "to", t.To, "via", t.Event)
//		return nil
//	}
type Transition[S ~string, E ~string] struct {
	// From is the state the machine is leaving.
	From S

	// To is the state the machine is entering.
	To S

	// Event is the event that resolved this edge.
	Event E
}

// EventMachine holds one current state and applies only the transitions its graph declares.
//
// It is not safe for concurrent use and holds no lock. Hooks perform real work, often I/O, so an internal mutex would
// be held across that work and would serialize every goroutine touching the machine. Callers synchronize instead, at
// the level where their own data already needs it.
//
// Build one with NewEventMachine or MustEventMachine.
//
// Example:
//
//	m := fsm.MustEventMachine(orderGraph, StateDraft)
//	err := m.Fire(ctx, EventSubmit)
type EventMachine[S ~string, E ~string] struct {
	graph   EventGraph[S, E]
	current S
}

// NewEventMachine returns a machine positioned at initial, or ErrUnknownState if the graph never names that state.
//
// Construction is the boundary where a state read from storage enters the program, and a state that drifted out of the
// graph would otherwise produce a machine that silently rejects every transition. Validating here turns that into an
// error at the point the bad value arrives.
//
// Example:
//
//	m, err := fsm.NewEventMachine(orderGraph, order.Status)
//	if err != nil {
//		// the stored status is not in the graph
//	}
func NewEventMachine[S, E ~string](graph EventGraph[S, E], initial S) (*EventMachine[S, E], error) {
	if !graph.hasState(initial) {
		return nil, fmt.Errorf("fsm: initial state %s: %w", initial, ErrUnknownState)
	}

	return &EventMachine[S, E]{graph: graph, current: initial}, nil
}

// MustEventMachine returns a machine positioned at initial and panics if the graph never names that state.
//
// Use it where the initial state is a compile-time constant and a failure would be a programmer mistake rather than bad
// data. Prefer NewEventMachine when the state came from storage or a request.
//
// Example:
//
//	m := fsm.MustEventMachine(orderGraph, StateDraft)
func MustEventMachine[S, E ~string](graph EventGraph[S, E], initial S) *EventMachine[S, E] {
	machine, err := NewEventMachine(graph, initial)
	if err != nil {
		panic(err)
	}

	return machine
}

// Current returns the state the machine is in.
//
// Example:
//
//	fmt.Println(m.Current())
//	// Draft
func (m *EventMachine[S, E]) Current() S {
	return m.current
}

// Is reports whether the machine is in state.
//
// The comparison does not consult the graph, so it works for any value of S.
//
// Example:
//
//	if m.Is(StateShipped) {
//		// ...
//	}
func (m *EventMachine[S, E]) Is(state S) bool {
	return m.current == state
}

// String returns the current state name, so EventMachine satisfies fmt.Stringer.
//
// Example:
//
//	fmt.Printf("%s", m)
//	// Draft
func (m *EventMachine[S, E]) String() string {
	return string(m.current)
}

// CanFire reports whether firing event is permitted from the current state, returning nil when it is.
//
// It never moves the machine. It reports whether the move is permitted, not that it will succeed: a blocking exit hook
// can still abort a transition that CanFire approved.
//
// Example:
//
//	if err := m.CanFire(ctx, EventShip); err != nil {
//		// not allowed from here, and err says why
//	}
func (m *EventMachine[S, E]) CanFire(_ context.Context, event E) error {
	_, err := m.resolve(event)

	return err
}

// Fire moves the machine along the edge that event declares from the current state.
//
// The state is unchanged when the edge does not exist, and the returned error wraps ErrInvalidTransition.
//
// Example:
//
//	if err := m.Fire(ctx, EventSubmit); err != nil {
//		// the graph declares no such edge from here
//	}
func (m *EventMachine[S, E]) Fire(_ context.Context, event E) error {
	transition, err := m.resolve(event)
	if err != nil {
		return err
	}

	m.current = transition.To

	return nil
}

// ForceState sets the current state directly, ignoring the graph.
//
// No edge is required, no guard is consulted, and no hook runs. This is a deliberate hole in every guarantee the rest
// of the package provides, and it exists for operational repair: support tooling, data migrations, and unsticking an
// entity a bug stranded. It is not for ordinary application code, where a declared transition is always the answer.
//
// The only check is that the graph names the state, so a typo cannot invent one.
//
// Example:
//
//	// in a repair script, not in a request handler
//	if err := m.ForceState(StateDraft); err != nil {
//		// the graph never declares that state
//	}
func (m *EventMachine[S, E]) ForceState(state S) error {
	if !m.graph.hasState(state) {
		return fmt.Errorf("fsm: force state %s: %w", state, ErrUnknownState)
	}

	m.current = state

	return nil
}

// resolve looks up the edge leaving the current state for event.
//
// A missing edge yields a PhaseResolve error whose To is the zero value, since no destination was found to name.
func (m *EventMachine[S, E]) resolve(event E) (Transition[S, E], error) {
	to, ok := m.graph.target(m.current, event)
	if !ok {
		return Transition[S, E]{From: m.current, Event: event}, &TransitionError[S, E]{
			From:  m.current,
			Event: event,
			Phase: PhaseResolve,
			Err:   ErrInvalidTransition,
		}
	}

	return Transition[S, E]{From: m.current, To: to, Event: event}, nil
}
