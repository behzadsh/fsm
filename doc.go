// Package fsm provides a finite state machine whose legal transitions are declared up front and checked at runtime.
//
// A graph declares the states and the transitions between them. A machine holds one current state and applies only the
// transitions the graph lists. States and events are the caller's own named string types, so one machine's vocabulary
// cannot be passed to another, and error messages stay readable.
//
// Declare the vocabularies, build the graph once, then drive a machine:
//
//	type OrderState string
//	type OrderEvent string
//
//	const (
//		StateDraft    OrderState = "Draft"
//		StateReview   OrderState = "Review"
//		StateCanceled OrderState = "Canceled"
//	)
//
//	const (
//		EventSubmit OrderEvent = "submit"
//		EventCancel OrderEvent = "cancel"
//	)
//
//	var orderGraph = fsm.NewEventGraph[OrderState, OrderEvent]().
//		On(StateDraft, EventSubmit, StateReview).
//		On(StateDraft, EventCancel, StateCanceled).
//		MustBuild()
//
//	m, err := fsm.NewEventMachine(orderGraph, StateDraft)
//	if err != nil {
//		// the initial state is not named by the graph
//	}
//
//	if err := m.Fire(ctx, EventSubmit); err != nil {
//		// the graph declares no such edge from the current state
//	}
//
// Errors carry the stage at which a transition failed; see Phase for what each stage implies about whether the machine
// moved and whether retrying is safe.
//
// # Concurrency
//
// A machine holds no lock and is not safe for concurrent use. Callers synchronize at the level where their own data
// already needs it.
package fsm
