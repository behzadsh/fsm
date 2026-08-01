package fsm

import (
	"errors"
	"fmt"
)

// Sentinel errors reported by this package. They are wrapped rather than returned directly, so a caller matching them
// with errors.Is still receives the detail TransitionError carries.
//
// Example:
//
//	if errors.Is(err, fsm.ErrInvalidTransition) {
//		// the graph declares no such edge
//	}
var (
	// ErrInvalidTransition reports that the graph declares no edge for the attempted move.
	ErrInvalidTransition = errors.New("invalid transition")

	// ErrReentrant reports that a hook tried to move the machine it is running inside.
	ErrReentrant = errors.New("reentrant call from hook")

	// ErrUnknownState reports a state that appears nowhere in the graph.
	ErrUnknownState = errors.New("unknown state")
)

// Phase identifies the stage of a transition at which it failed.
//
// PhaseResolve and PhaseGuard mean nothing moved and nothing ran. PhaseExit means nothing moved, though an exit hook
// may already have had an effect. PhaseEnter means the state changed and a hook failed afterwards.
//
// The zero value is PhaseResolve. Changing the state is not a phase: it is one assignment that cannot fail. Use
// TransitionError.Moved rather than the phase to tell whether the state changed, since a reporting exit hook fails at
// PhaseExit and the machine still moves.
//
// The numeric values may shift if a stage is added. Store and label phases with String, not with uint8(phase).
//
// Example:
//
//	switch te.Phase {
//	case fsm.PhaseGuard:
//		// a rule refused; nothing happened
//	case fsm.PhaseEnter:
//		// the move succeeded; do not retry it
//	}
type Phase uint8

// The stages of a transition, in the order they run.
const (
	// PhaseResolve is the lookup of the edge for the current state and the given event.
	PhaseResolve Phase = iota

	// PhaseGuard is the guard registered on the resolved edge.
	PhaseGuard

	// PhaseExit is the hook leaving the source state. It runs before the state changes.
	PhaseExit

	// PhaseEnter is the hook entering the target state. It runs after the state has changed.
	PhaseEnter
)

// String returns the phase name in lower case, suitable for an error message or a metrics label.
//
// Example:
//
//	fmt.Println(fsm.PhaseGuard)
//	// guard
func (p Phase) String() string {
	switch p {
	case PhaseResolve:
		return "resolve"
	case PhaseGuard:
		return "guard"
	case PhaseExit:
		return "exit"
	case PhaseEnter:
		return "enter"
	default:
		return fmt.Sprintf("Phase(%d)", uint8(p))
	}
}

// TransitionError describes a transition that did not complete, and how far it got.
//
// To is the zero value when resolve failed, since no edge was found and no destination is known. In every other phase
// both ends are named.
//
// Retrieve it with errors.As. Read Moved to tell whether the state changed.
//
// Example:
//
//	var te *fsm.TransitionError[OrderState, OrderEvent]
//	if errors.As(err, &te) && te.Phase == fsm.PhaseEnter {
//		// the order did move; only the notification failed
//	}
type TransitionError[S ~string, E ~string] struct {
	// From is the state the machine was in when the transition was attempted.
	From S

	// To is the target of the resolved edge, or the zero value when resolve failed.
	To S

	// Event is the event that was fired.
	Event E

	// Phase is the stage at which the transition failed.
	Phase Phase

	// Committed records whether the state had already changed when this error was produced. Moved reports it. It
	// cannot be derived from Phase alone, because a reporting exit hook fails at PhaseExit without stopping the
	// transition.
	Committed bool

	// Err is the underlying cause, and is what Unwrap returns.
	Err error
}

// Error renders the failure, naming a destination only when resolve found one.
//
// Example:
//
//	// fsm: cannot fire pay from Draft: invalid transition
//	// fsm: exit Paid -> Shipped (event ship): hold not released
func (e *TransitionError[S, E]) Error() string {
	var zero S
	if e.To == zero {
		return fmt.Sprintf("fsm: cannot fire %s from %s: %v", e.Event, e.From, e.Err)
	}

	return fmt.Sprintf("fsm: %s %s -> %s (event %s): %v", e.Phase, e.From, e.To, e.Event, e.Err)
}

// Unwrap returns the underlying cause, so errors.Is reaches this package's sentinels and any error a guard or hook
// returned.
//
// Example:
//
//	errors.Is(err, fsm.ErrInvalidTransition)
func (e *TransitionError[S, E]) Unwrap() error {
	return e.Err
}

// Moved reports whether the state changed before the failure.
//
// False means nothing moved and the call can be retried. True means the transition took effect and a reported hook
// failed afterwards, so retrying would repeat work that already happened.
//
// This is not the same as which phase failed. A reporting exit hook fails at PhaseExit without stopping the
// transition, so the state changes and Moved returns true.
//
// Example:
//
//	var te *fsm.TransitionError[OrderState, OrderEvent]
//	if errors.As(err, &te) && te.Moved() {
//		// the order moved; retrying would repeat it
//	}
func (e *TransitionError[S, E]) Moved() bool {
	return e.Committed
}
