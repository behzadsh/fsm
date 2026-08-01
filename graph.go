package fsm

import (
	"errors"
	"fmt"
)

// EventGraph declares the states of a machine and the labeled transitions between them.
//
// Each edge is identified by the pair (from, event) and resolves to exactly one target state. An event may fan out, so
// the same event name may lead to different targets from different source states, and several events may reach the same
// target. A state with no outgoing edge is terminal; terminal states need not be declared, since they are simply
// targets nothing leaves.
//
// The zero value is an empty graph that permits no transition. Build one with NewEventGraph. A built graph is sealed:
// it holds its own copy of the edges, so later use of the builder that produced it cannot change what it allows.
//
// Example:
//
//	graph := fsm.NewEventGraph[OrderState, OrderEvent]().
//		On(StateDraft, EventSubmit, StateReview).
//		MustBuild()
type EventGraph[S ~string, E ~string] struct {
	edges map[S]map[E]S

	// states is every state named by the graph, whether as the source of an edge or only as a target. It is the
	// universe a machine's state is validated against.
	states map[S]struct{}
}

// hasState reports whether the graph names the state at all, as the source of an edge or only as a target.
func (g EventGraph[S, E]) hasState(state S) bool {
	_, ok := g.states[state]

	return ok
}

// target returns the state the edge (from, event) leads to, and whether that edge is declared.
func (g EventGraph[S, E]) target(from S, event E) (S, bool) {
	to, ok := g.edges[from][event]

	return to, ok
}

// NewEventGraph returns an empty builder for a graph whose states are of type S and whose events are of type E.
//
// Both type parameters are constrained to ~string, so callers declare their own named string types. That keeps one
// machine's states from being accepted by another, while error messages stay readable and stored values round-trip
// without conversion.
//
// Example:
//
//	type OrderState string
//	type OrderEvent string
//
//	builder := fsm.NewEventGraph[OrderState, OrderEvent]()
func NewEventGraph[S, E ~string]() *EventGraphBuilder[S, E] {
	return &EventGraphBuilder[S, E]{edges: map[S]map[E]S{}}
}

// EventGraphBuilder accumulates edges and the conflicts found while declaring them.
//
// Every method returns the builder, so declarations chain. Conflicts are not reported as they happen; they are
// collected and surfaced together by Build or MustBuild.
//
// Example:
//
//	builder := fsm.NewEventGraph[OrderState, OrderEvent]()
//	builder = builder.On(StateDraft, EventSubmit, StateReview)
//	graph, err := builder.Build()
type EventGraphBuilder[S ~string, E ~string] struct {
	edges map[S]map[E]S
	errs  []error
}

// On declares that firing event while in from moves the machine to.
//
// Declaring the pair (from, event) more than once is a conflict, recorded and reported by Build or MustBuild. The first
// declaration wins, so a conflicting later one never silently replaces an earlier edge. Re-declaring an identical edge
// is a conflict too: it is a copy-paste mistake rather than an intent to change anything.
//
// Example:
//
//	builder.On(StateDraft, EventSubmit, StateReview)
//	// firing EventSubmit in StateDraft now leads to StateReview
func (b *EventGraphBuilder[S, E]) On(from S, event E, to S) *EventGraphBuilder[S, E] {
	if b.edges[from] == nil {
		b.edges[from] = map[E]S{}
	}

	if prev, exists := b.edges[from][event]; exists {
		b.errs = append(b.errs, fmt.Errorf("duplicate edge (%s, %s): -> %s and -> %s", from, event, prev, to))

		return b
	}

	b.edges[from][event] = to

	return b
}

// Build returns the declared graph, or every conflict found while declaring it.
//
// Conflicts are joined with errors.Join, so a graph with several mistakes reports all of them at once rather than one
// per attempt. Use Build when the graph is assembled conditionally and the error has somewhere to go; use MustBuild for
// a package-level variable.
//
// The graph is returned even when the error is non-nil, holding the edges declared before each conflict. That makes the
// first-declaration-wins rule observable rather than theoretical.
//
// Example:
//
//	graph, err := builder.Build()
//	if err != nil {
//		// duplicate edge (Draft, submit): -> Review and -> Paid
//	}
func (b *EventGraphBuilder[S, E]) Build() (EventGraph[S, E], error) {
	graph := EventGraph[S, E]{
		edges:  make(map[S]map[E]S, len(b.edges)),
		states: make(map[S]struct{}, len(b.edges)),
	}

	for from, byEvent := range b.edges {
		targets := make(map[E]S, len(byEvent))
		for event, to := range byEvent {
			targets[event] = to
			graph.states[to] = struct{}{}
		}

		graph.edges[from] = targets
		graph.states[from] = struct{}{}
	}

	if len(b.errs) > 0 {
		return graph, errors.Join(b.errs...)
	}

	return graph, nil
}

// MustBuild returns the declared graph and panics if any conflict was found.
//
// A graph is static configuration built once at start-up, so a conflict is a programmer mistake that should surface
// immediately and loudly rather than being handled. This is the regexp.MustCompile pattern, and it is what a
// package-level variable needs, since a variable initializer cannot handle an error.
//
// Example:
//
//	var orderGraph = fsm.NewEventGraph[OrderState, OrderEvent]().
//		On(StateDraft, EventSubmit, StateReview).
//		MustBuild()
func (b *EventGraphBuilder[S, E]) MustBuild() EventGraph[S, E] {
	graph, err := b.Build()
	if err != nil {
		panic(err)
	}

	return graph
}

// Graph declares the states of a machine and the transitions between them, without naming the transitions.
//
// This is the simple surface. An edge is identified by the pair (from, to), and a machine moves by naming its
// destination. Use EventGraph instead when one action must lead to different targets depending on where the machine
// is, since expressing that requires naming the action.
//
// The zero value is an empty graph that permits no transition. Build one with NewGraph. A built graph is sealed, and
// holds its own copy of the edges.
//
// Example:
//
//	graph := fsm.NewGraph[OrderState]().
//		To(StateDraft, StateReview, StateCanceled).
//		MustBuild()
type Graph[S ~string] struct {
	inner EventGraph[S, S]
}

// NewGraph returns an empty builder for a graph whose states are of type S.
//
// Example:
//
//	type OrderState string
//
//	builder := fsm.NewGraph[OrderState]()
func NewGraph[S ~string]() *GraphBuilder[S] {
	return &GraphBuilder[S]{
		inner: NewEventGraph[S, S](),
		seen:  map[S]map[S]struct{}{},
	}
}

// GraphBuilder accumulates edges and the conflicts found while declaring them.
//
// Every method returns the builder, so declarations chain. Conflicts are collected and surfaced together by Build or
// MustBuild.
//
// Example:
//
//	builder := fsm.NewGraph[OrderState]()
//	builder = builder.To(StateDraft, StateReview)
//	graph, err := builder.Build()
type GraphBuilder[S ~string] struct {
	inner *EventGraphBuilder[S, S]
	seen  map[S]map[S]struct{}
	errs  []error
}

// To declares that the machine may move from from to each of the given targets.
//
// It is variadic because one state usually leads to several, and reads as a row of a transition table. Declaring the
// same pair (from, to) more than once is a conflict, reported by Build or MustBuild; calling To with no targets
// declares nothing at all.
//
// Example:
//
//	builder.To(StateDraft, StateReview, StateCanceled)
func (b *GraphBuilder[S]) To(from S, to ...S) *GraphBuilder[S] {
	if b.seen[from] == nil {
		b.seen[from] = map[S]struct{}{}
	}

	for _, target := range to {
		if _, exists := b.seen[from][target]; exists {
			b.errs = append(b.errs, fmt.Errorf("duplicate edge %s -> %s", from, target))

			continue
		}

		b.seen[from][target] = struct{}{}

		// Each edge is stored with its target state as the event name, which is what lets the simple surface reuse
		// the labeled engine unchanged. Nothing outside this package can observe the binding.
		b.inner.On(from, target, target)
	}

	return b
}

// Build returns the declared graph, or every conflict found while declaring it.
//
// Conflicts are joined with errors.Join. Use Build when the graph is assembled conditionally; use MustBuild for a
// package-level variable.
//
// Example:
//
//	graph, err := builder.Build()
//	if err != nil {
//		// duplicate edge Draft -> Review
//	}
func (b *GraphBuilder[S]) Build() (Graph[S], error) {
	inner, err := b.inner.Build()

	graph := Graph[S]{inner: inner}

	if len(b.errs) > 0 {
		return graph, errors.Join(b.errs...)
	}

	return graph, err
}

// MustBuild returns the declared graph and panics if any conflict was found.
//
// Example:
//
//	var orderGraph = fsm.NewGraph[OrderState]().
//		To(StateDraft, StateReview).
//		MustBuild()
func (b *GraphBuilder[S]) MustBuild() Graph[S] {
	graph, err := b.Build()
	if err != nil {
		panic(err)
	}

	return graph
}
