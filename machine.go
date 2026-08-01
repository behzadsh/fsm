package fsm

import (
	"context"
	"errors"
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

// failure wraps err as a TransitionError describing this move at the given phase.
func (t Transition[S, E]) failure(phase Phase, committed bool, err error) *TransitionError[S, E] {
	return &TransitionError[S, E]{
		From:      t.From,
		To:        t.To,
		Event:     t.Event,
		Phase:     phase,
		Committed: committed,
		Err:       err,
	}
}

// Hook is a guard or a lifecycle callback. It receives the whole transition, so it can act on the move rather than only
// on the state it was registered against.
//
// A guard must not have side effects, because CanFire consults it speculatively without the move happening. Exit and
// enter hooks may do real work, including I/O; they run only for a transition that is actually taking place.
//
// Example:
//
//	func(ctx context.Context, t fsm.Transition[OrderState, OrderEvent]) error {
//		return notify(ctx, t.To)
//	}
type Hook[S ~string, E ~string] func(context.Context, Transition[S, E]) error

// exitHook is the single exit slot for a state: the function, and whether its error aborts the transition.
type exitHook[S ~string, E ~string] struct {
	fn     Hook[S, E]
	blocks bool
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

	// guards is keyed by edge, so two events reaching the same target stay independent.
	guards map[S]map[E]Hook[S, E]

	// onExit and onEnter hold at most one hook per state; registering again replaces the previous one.
	onExit  map[S]exitHook[S, E]
	onEnter map[S]Hook[S, E]

	// inFlight is set for the duration of a transition, so a hook cannot move the machine from under it.
	inFlight bool
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

	return &EventMachine[S, E]{
		graph:   graph,
		current: initial,
		guards:  map[S]map[E]Hook[S, E]{},
		onExit:  map[S]exitHook[S, E]{},
		onEnter: map[S]Hook[S, E]{},
	}, nil
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

// Guard registers a predicate on the single edge (from, event).
//
// The guard decides whether the move is permitted. It runs before anything with a side effect, so refusing it leaves
// the machine untouched, and it is the only thing that can block a transition that CanFire approved.
//
// It must not have side effects: CanFire consults it speculatively, without the move happening. Guards key on the edge,
// so two events reaching the same target keep separate guards. Registering again replaces the previous guard.
//
// Example:
//
//	m.Guard(StatePaid, EventShip, func(ctx context.Context, t fsm.Transition[OrderState, OrderEvent]) error {
//		if order.Address == "" {
//			return ErrNoAddress
//		}
//		return nil
//	})
func (m *EventMachine[S, E]) Guard(from S, event E, hook Hook[S, E]) *EventMachine[S, E] {
	if m.guards[from] == nil {
		m.guards[from] = map[E]Hook[S, E]{}
	}

	m.guards[from][event] = hook

	return m
}

// OnExit registers the hook that runs when the machine leaves state, reporting its error without stopping the move.
//
// The transition commits regardless, so the returned error reaches the caller as information: the machine moved, and
// something alongside it failed. Use OnExitBlocking when the failure should prevent the move instead.
//
// A state has one exit hook. OnExit and OnExitBlocking share that slot, so whichever is called last decides both the
// function and whether it blocks. Overwriting is silent.
//
// Example:
//
//	m.OnExit(StatePaid, func(ctx context.Context, t fsm.Transition[OrderState, OrderEvent]) error {
//		return pushMetrics(ctx, t.From, t.To)
//	})
func (m *EventMachine[S, E]) OnExit(state S, hook Hook[S, E]) *EventMachine[S, E] {
	m.onExit[state] = exitHook[S, E]{fn: hook, blocks: false}

	return m
}

// OnExitBlocking registers the hook that runs when the machine leaves state, aborting the transition if it fails.
//
// It runs before the commit, so an abort leaves the machine exactly where it was and the enter hook never runs. This is
// where fallible work belongs when its failure should prevent the move: the transition simply does not happen, and
// there is nothing to undo.
//
// A state has one exit hook, shared with OnExit; see OnExit for the overwrite rule.
//
// Example:
//
//	m.OnExitBlocking(StatePaid, func(ctx context.Context, t fsm.Transition[OrderState, OrderEvent]) error {
//		return releaseHold(ctx, order)
//	})
func (m *EventMachine[S, E]) OnExitBlocking(state S, hook Hook[S, E]) *EventMachine[S, E] {
	m.onExit[state] = exitHook[S, E]{fn: hook, blocks: true}

	return m
}

// OnEnter registers the hook that runs once the machine has entered state.
//
// It runs after the commit, so it cannot stop the transition. Its error is reported to the caller, and Moved on that
// error returns true: the move happened, and only the follow-up failed. Blocking here is not offered, because undoing a
// committed transition would mean reverting a state change whose exit hook already took effect in the outside world.
//
// A state has one enter hook; registering again replaces it, silently.
//
// Example:
//
//	m.OnEnter(StateShipped, func(ctx context.Context, t fsm.Transition[OrderState, OrderEvent]) error {
//		return notify(ctx, order)
//	})
func (m *EventMachine[S, E]) OnEnter(state S, hook Hook[S, E]) *EventMachine[S, E] {
	m.onEnter[state] = hook

	return m
}

// CanFire reports whether firing event is permitted from the current state, returning nil when it is.
//
// It checks that the edge exists and asks the guard registered on it, and never moves the machine. It reports whether
// the move is permitted, not that it will succeed: a blocking exit hook can still abort a transition CanFire approved.
//
// Example:
//
//	if err := m.CanFire(ctx, EventShip); err != nil {
//		// not allowed from here, and err says why
//	}
func (m *EventMachine[S, E]) CanFire(ctx context.Context, event E) error {
	transition, err := m.resolve(event)
	if err != nil {
		return err
	}

	return m.runGuard(ctx, transition)
}

// Fire moves the machine along the edge that event declares from the current state.
//
// The stages run in a fixed order: the edge is resolved, the guard is consulted, the exit hook runs, the state changes,
// and the enter hook runs. Failing to resolve, a refusing guard, and a blocking exit hook each leave the machine where
// it was. Errors from a reporting exit hook and from the enter hook are joined and returned after the move has taken
// effect.
//
// Reading the returned error with errors.As gives a TransitionError whose Moved reports whether the state changed,
// which is what decides if retrying is safe.
//
// Calling Fire from inside a hook returns ErrReentrant and does nothing: the outer transition already holds the
// machine, and a nested commit would be silently overwritten when it resumes.
//
// Example:
//
//	if err := m.Fire(ctx, EventSubmit); err != nil {
//		var te *fsm.TransitionError[OrderState, OrderEvent]
//		if errors.As(err, &te) && !te.Moved() {
//			// nothing happened; safe to retry
//		}
//	}
func (m *EventMachine[S, E]) Fire(ctx context.Context, event E) error {
	if m.inFlight {
		return ErrReentrant
	}

	m.inFlight = true
	defer func() { m.inFlight = false }()

	transition, err := m.resolve(event)
	if err != nil {
		return err
	}

	if err := m.runGuard(ctx, transition); err != nil {
		return err
	}

	var reported error

	if hook, ok := m.onExit[transition.From]; ok && hook.fn != nil {
		if err = hook.fn(ctx, transition); err != nil {
			if hook.blocks {
				return transition.failure(PhaseExit, false, err)
			}

			reported = transition.failure(PhaseExit, true, err)
		}
	}

	m.current = transition.To

	if hook := m.onEnter[transition.To]; hook != nil {
		if err = hook(ctx, transition); err != nil {
			reported = errors.Join(reported, transition.failure(PhaseEnter, true, err))
		}
	}

	return reported
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

// runGuard runs the guard registered on the transition's edge, if any.
func (m *EventMachine[S, E]) runGuard(ctx context.Context, transition Transition[S, E]) error {
	hook := m.guards[transition.From][transition.Event]
	if hook == nil {
		return nil
	}

	if err := hook(ctx, transition); err != nil {
		return transition.failure(PhaseGuard, false, err)
	}

	return nil
}
