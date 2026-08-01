package fsm

import (
	"context"
	"errors"
	"fmt"
)

// Transition describes one move: where it started, where it leads, and the event that caused it.
//
// It is the value passed to guards and hooks, so they see the whole move rather than only the state they were
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

// Hook is a guard or a lifecycle callback.
//
// A guard must not have side effects, because CanFire calls it without moving the machine. Exit and enter hooks may do
// real work, including I/O, and run only for a transition that is taking place.
//
// Example:
//
//	func(ctx context.Context, t fsm.Transition[OrderState, OrderEvent]) error {
//		return notify(ctx, t.To)
//	}
type Hook[S ~string, E ~string] func(context.Context, Transition[S, E]) error

// exitHook is the exit slot for a state: the function, and whether its error aborts the transition.
type exitHook[S ~string, E ~string] struct {
	fn     Hook[S, E]
	blocks bool
}

// EventMachine holds one current state and applies only the transitions its graph declares.
//
// It holds no lock and is not safe for concurrent use. Hooks perform real work, often I/O, and an internal mutex would
// be held across it. Callers synchronize instead.
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

	// guards is keyed by edge, so two events reaching the same target keep separate guards.
	guards map[S]map[E]Hook[S, E]

	// onExit and onEnter hold at most one hook per state; registering again replaces the previous one.
	onExit  map[S]exitHook[S, E]
	onEnter map[S]Hook[S, E]

	// inFlight is set for the duration of a transition, so a hook cannot move the machine while one is running.
	inFlight bool
}

// NewEventMachine returns a machine positioned at initial, or ErrUnknownState if the graph never names that state.
//
// Construction is where a state read from storage enters the program. Without this check, a state that has drifted out
// of the graph produces a machine that rejects every transition, with nothing to say why.
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
// Use it where the initial state is a constant and a failure would be a programmer mistake. Prefer NewEventMachine
// when the state came from storage or a request.
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
// The comparison does not consult the graph, so any value of S may be passed.
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
// The guard reports whether the move is allowed. It runs before anything with a side effect, so a refusal leaves the
// machine untouched.
//
// It must not have side effects, because CanFire calls it without moving the machine. Guards key on the edge, so two
// events reaching the same target keep separate guards. Registering again replaces the previous guard.
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
// The state changes regardless, and the returned error reaches the caller alongside that fact. Use OnExitBlocking when
// the failure should prevent the move.
//
// A state has one exit hook, shared with OnExitBlocking. Whichever is called last decides both the function and whether
// it blocks. Registering again replaces the previous hook.
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
// It runs before the state changes, so an abort leaves the machine where it was and the enter hook does not run. Work
// that should prevent a move when it fails belongs here: the transition does not happen, leaving nothing to undo.
//
// A state has one exit hook, shared with OnExit. See OnExit for the replacement rule.
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
// It runs after the state has changed and cannot stop the transition. Its error is reported to the caller, and Moved
// on that error returns true. Blocking is not offered here: undoing the transition would mean reverting a state change
// whose exit hook has already taken effect outside the machine.
//
// A state has one enter hook. Registering again replaces it.
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
// It checks that the edge exists and calls the guard registered on it, without moving the machine. It reports that the
// move is allowed, not that it will succeed: a blocking exit hook can still abort it.
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
// The stages run in order: the edge is resolved, the guard is called, the exit hook runs, the state changes, and the
// enter hook runs. A failed resolve, a refusing guard, and a blocking exit hook each leave the machine where it was.
// Errors from a reporting exit hook and from the enter hook are joined and returned after the state has changed.
//
// Reading the returned error with errors.As gives a TransitionError whose Moved reports whether the state changed.
//
// Calling Fire from inside a hook returns ErrReentrant and does nothing. The outer transition resolved its edge before
// the hook ran, and would overwrite a nested change when it resumes.
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

	if err = m.runGuard(ctx, transition); err != nil {
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
// No edge is required, no guard is called, and no hook runs, which sets aside every guarantee the rest of the package
// provides. It is meant for operational repair: support tooling, data migrations, and freeing an entity a bug stranded.
// Ordinary application code should declare the transition instead.
//
// The one check is that the graph names the state, so a typo cannot invent one.
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

// Change describes one move on the simple surface: the state left and the state entered.
//
// It is what guards and hooks receive there. Unlike Transition it carries no event, since that surface has none.
//
// Example:
//
//	func(ctx context.Context, c fsm.Change[OrderState]) error {
//		log.Info("moved", "from", c.From, "to", c.To)
//		return nil
//	}
type Change[S ~string] struct {
	// From is the state the machine is leaving.
	From S

	// To is the state the machine is entering.
	To S
}

// StateHook is a guard or a lifecycle callback on the simple surface.
//
// The rules match Hook: a guard must not have side effects, because CanTransitionTo calls it without moving the
// machine, while exit and enter hooks may do real work and run only for a transition that is taking place.
//
// Example:
//
//	func(ctx context.Context, c fsm.Change[OrderState]) error {
//		return notify(ctx, c.To)
//	}
type StateHook[S ~string] func(context.Context, Change[S]) error

// Machine holds one current state and applies only the transitions its graph declares, naming them by destination.
//
// It is built on the same engine as EventMachine, with each edge's event name bound to its target state. That binding
// does not appear in its types, methods, error messages, or hook arguments.
//
// Like EventMachine it holds no lock and is not safe for concurrent use. Build one with New or MustNew.
//
// Example:
//
//	m := fsm.MustNew(orderGraph, StateDraft)
//	err := m.TransitionTo(ctx, StateReview)
type Machine[S ~string] struct {
	inner *EventMachine[S, S]
}

// New returns a machine positioned at initial, or ErrUnknownState if the graph never names that state.
//
// As with NewEventMachine, the check matters because construction is where a state read from storage enters the
// program.
//
// Example:
//
//	m, err := fsm.New(orderGraph, order.Status)
//	if err != nil {
//		// the stored status is not in the graph
//	}
func New[S ~string](graph Graph[S], initial S) (*Machine[S], error) {
	inner, err := NewEventMachine(graph.inner, initial)
	if err != nil {
		return nil, err
	}

	return &Machine[S]{inner: inner}, nil
}

// MustNew returns a machine positioned at initial and panics if the graph never names that state.
//
// Use it where the initial state is a constant. Prefer New when the state came from storage or a request.
//
// Example:
//
//	m := fsm.MustNew(orderGraph, StateDraft)
func MustNew[S ~string](graph Graph[S], initial S) *Machine[S] {
	machine, err := New(graph, initial)
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
func (m *Machine[S]) Current() S {
	return m.inner.Current()
}

// Is reports whether the machine is in state.
//
// Example:
//
//	if m.Is(StateShipped) {
//		// ...
//	}
func (m *Machine[S]) Is(state S) bool {
	return m.inner.Is(state)
}

// String returns the current state name, so Machine satisfies fmt.Stringer.
//
// Example:
//
//	fmt.Printf("%s", m)
//	// Draft
func (m *Machine[S]) String() string {
	return m.inner.String()
}

// Guard registers a predicate on the single edge from -> to.
//
// It must not have side effects, because CanTransitionTo calls it without moving the machine. Registering again
// replaces the previous guard.
//
// Example:
//
//	m.Guard(StateReview, StatePaid, func(ctx context.Context, c fsm.Change[OrderState]) error {
//		if order.Total == 0 {
//			return ErrNothingToPay
//		}
//		return nil
//	})
func (m *Machine[S]) Guard(from, to S, hook StateHook[S]) *Machine[S] {
	m.inner.Guard(from, to, adapt(hook))

	return m
}

// OnExit registers the hook that runs when the machine leaves state, reporting its error without stopping the move.
//
// A state has one exit hook, shared with OnExitBlocking. Whichever is called last decides both the function and
// whether it blocks. Registering again replaces the previous hook.
//
// Example:
//
//	m.OnExit(StatePaid, func(ctx context.Context, c fsm.Change[OrderState]) error {
//		return pushMetrics(ctx, c.From, c.To)
//	})
func (m *Machine[S]) OnExit(state S, hook StateHook[S]) *Machine[S] {
	m.inner.OnExit(state, adapt(hook))

	return m
}

// OnExitBlocking registers the hook that runs when the machine leaves state, aborting the transition if it fails.
//
// It runs before the state changes, so an abort leaves the machine where it was and the enter hook does not run.
//
// Example:
//
//	m.OnExitBlocking(StatePaid, func(ctx context.Context, c fsm.Change[OrderState]) error {
//		return releaseHold(ctx, order)
//	})
func (m *Machine[S]) OnExitBlocking(state S, hook StateHook[S]) *Machine[S] {
	m.inner.OnExitBlocking(state, adapt(hook))

	return m
}

// OnEnter registers the hook that runs once the machine has entered state.
//
// It runs after the state has changed and cannot stop the transition. Its error is reported to the caller.
//
// Example:
//
//	m.OnEnter(StateShipped, func(ctx context.Context, c fsm.Change[OrderState]) error {
//		return notify(ctx, order)
//	})
func (m *Machine[S]) OnEnter(state S, hook StateHook[S]) *Machine[S] {
	m.inner.OnEnter(state, adapt(hook))

	return m
}

// CanTransitionTo reports whether moving to the given state is permitted, returning nil when it is.
//
// It checks that the edge exists and calls the guard registered on it, without moving the machine. It reports that the
// move is allowed, not that it will succeed: a blocking exit hook can still abort it.
//
// Example:
//
//	if err := m.CanTransitionTo(ctx, StateShipped); err != nil {
//		// not allowed from here, and err says why
//	}
func (m *Machine[S]) CanTransitionTo(ctx context.Context, to S) error {
	return simplify(to, m.inner.CanFire(ctx, to))
}

// TransitionTo moves the machine to the given state, if the graph declares an edge leading there.
//
// The stages run in the same order as on the labeled surface: the edge is resolved, the guard is called, the exit hook
// runs, the state changes, and the enter hook runs. Calling it from inside a hook returns ErrReentrant.
//
// Example:
//
//	if err := m.TransitionTo(ctx, StateReview); err != nil {
//		// fsm: cannot transition Draft -> Review: invalid transition
//	}
func (m *Machine[S]) TransitionTo(ctx context.Context, to S) error {
	return simplify(to, m.inner.Fire(ctx, to))
}

// ForceState sets the current state directly, ignoring the graph.
//
// No edge is required, no guard is called, and no hook runs. As on the labeled surface it is meant for operational
// repair, and its one check is that the graph names the state.
//
// Example:
//
//	// in a repair script, not in a request handler
//	if err := m.ForceState(StateDraft); err != nil {
//		// the graph never declares that state
//	}
func (m *Machine[S]) ForceState(state S) error {
	return m.inner.ForceState(state)
}

// adapt turns a StateHook into the Hook the engine expects, dropping the event the simple surface does not have.
func adapt[S ~string](hook StateHook[S]) Hook[S, S] {
	if hook == nil {
		return nil
	}

	return func(ctx context.Context, transition Transition[S, S]) error {
		return hook(ctx, Change[S]{From: transition.From, To: transition.To})
	}
}

// simplify rewrites an engine error so it never mentions events.
//
// A resolve failure is the interesting case: the engine found no edge, so it has no target to name, but the caller
// named one explicitly, and it is restored here. Joined errors are rewritten element by element, so an exit and an
// enter failure in the same call both survive.
func simplify[S ~string](to S, err error) error {
	if err == nil {
		return nil
	}

	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts := joined.Unwrap()
		rewritten := make([]error, 0, len(parts))

		for _, part := range parts {
			rewritten = append(rewritten, simplify[S](to, part))
		}

		return errors.Join(rewritten...)
	}

	var transitionErr *TransitionError[S, S]
	if !errors.As(err, &transitionErr) {
		return err
	}

	if transitionErr.Phase == PhaseResolve {
		return fmt.Errorf("fsm: cannot transition %s -> %s: %w", transitionErr.From, to, transitionErr.Err)
	}

	return fmt.Errorf("fsm: %s %s -> %s: %w", transitionErr.Phase, transitionErr.From, transitionErr.To, transitionErr.Err)
}
