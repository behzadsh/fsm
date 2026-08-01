// Package fsm provides a finite state machine.
//
// A graph declares the states and the transitions allowed between them. A machine holds one current state and applies
// only the transitions the graph lists. States and events are the caller's own named string types, so the compiler
// rejects one machine's vocabulary where another's is expected.
//
// The package offers two surfaces. They share an engine but do not mix: a graph belongs to one or the other.
//
// The simple surface names the destination:
//
//	var graph = fsm.NewGraph[OrderState]().
//		To(StateDraft, StateReview, StateCanceled).
//		To(StateReview, StatePaid, StateCanceled).
//		MustBuild()
//
//	m := fsm.MustNew(graph, StateDraft)
//	err := m.TransitionTo(ctx, StateReview)
//
// The labeled surface names the action, and the graph decides where it leads. An action may lead to different states
// depending on where the machine is, which the simple surface cannot express:
//
//	var graph = fsm.NewEventGraph[OrderState, OrderEvent]().
//		On(StateDraft, EventCancel, StateCanceled).
//		On(StatePaid, EventCancel, StateRefunded).
//		On(StateReview, EventPay, StatePaid).
//		MustBuild()
//
//	m := fsm.MustEventMachine(graph, StateDraft)
//	err := m.Fire(ctx, EventCancel)
//
// # Guards and hooks
//
// A transition resolves the edge, consults the guard, runs the exit hook, changes the state, and runs the enter hook,
// in that order.
//
// A guard reports whether the move is allowed. CanFire and CanTransitionTo call it without moving the machine, so it
// must not have side effects. An exit hook registered with OnExitBlocking aborts the transition when it fails, before
// the state changes; one registered with OnExit has its error reported instead. An enter hook runs after the state has
// changed and cannot stop it.
//
// A guard is registered per edge and a hook per state and phase. Registering again replaces the previous one.
//
// # Errors
//
// The labeled surface returns TransitionError, which carries the phase that failed. Its Moved method reports whether
// the state changed before the failure. The simple surface returns errors built with fmt.Errorf, wrapping the same
// sentinels; errors.Is matches, errors.As does not.
//
// # Limits
//
// CanFire and CanTransitionTo report that a move is allowed, not that it will succeed. They check the edge and the
// guard. A blocking exit hook can still abort the transition.
//
// A machine holds no lock and is not safe for concurrent use. Callers synchronize.
//
// ForceState sets the state without consulting the graph, guards, or hooks. It is meant for operational repair.
package fsm
