package fsm

import (
	"errors"
	"fmt"
)

// Sentinel errors reported by this package. Match them with errors.Is; they are wrapped rather than returned directly,
// so the caller also gets the structured detail carried by TransitionError.
//
// Example:
//
//	if errors.Is(err, fsm.ErrInvalidTransition) {
//		// the graph declares no such edge
//	}
var (
	// ErrInvalidTransition reports that the graph declares no edge for the attempted move.
	ErrInvalidTransition = errors.New("invalid transition")

	// ErrReentrant reports that a hook tried to move the machine that is already mid-transition.
	ErrReentrant = errors.New("reentrant call from hook")

	// ErrUnknownState reports a state that appears nowhere in the graph.
	ErrUnknownState = errors.New("unknown state")
)

// Phase identifies the stage of a transition at which something failed.
//
// It answers the only question a caller has after a failure: did anything actually happen? PhaseResolve and PhaseGuard
// both mean nothing moved and no side effect ran, so retrying later is safe. PhaseExit means nothing moved but an exit
// hook may already have had an effect. PhaseEnter means the machine did move and only the follow-up failed, so the
// caller should log rather than retry.
//
// The zero value is PhaseResolve, the earliest stage. Committing is not a phase, because it is a single assignment
// that cannot fail; the boundary it draws is reported by TransitionError.Moved instead.
//
// The numeric values are an implementation detail and may shift if a stage is ever added. Persist and label phases with
// String, not with uint8(phase).
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

	// PhaseExit is the hook leaving the source state, which runs before the state changes.
	PhaseExit

	// PhaseEnter is the hook entering the target state, which runs after the state has changed.
	PhaseEnter
)

// String returns the phase name in lower case, so it reads well both in an error message and as a metrics label.
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
// To is the zero value when the failure was a failed resolve, since no edge was found and therefore no destination is
// known. In every other phase both ends are named.
//
// Retrieve it with errors.As, and read Phase to decide what to do; see Phase for what each value implies about whether
// the machine moved.
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

	// Err is the underlying cause, and is what Unwrap returns.
	Err error
}

// Error renders the failure, naming a destination only when one was resolved.
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

// Unwrap returns the underlying cause, so errors.Is reaches both this package's sentinels and any error a guard or hook
// returned.
//
// Example:
//
//	errors.Is(err, fsm.ErrInvalidTransition)
func (e *TransitionError[S, E]) Unwrap() error {
	return e.Err
}

// Moved reports whether the machine's state actually changed before the failure.
//
// It draws the commit boundary that decides what a caller should do next. False means nothing moved and retrying is
// safe. True means the transition succeeded and only a post-commit hook failed, so the work must not be retried; log it
// instead.
//
// Example:
//
//	var te *fsm.TransitionError[OrderState, OrderEvent]
//	if errors.As(err, &te) && te.Moved() {
//		// the order did move; do not retry, or it happens twice
//	}
func (e *TransitionError[S, E]) Moved() bool {
	return e.Phase == PhaseEnter
}
