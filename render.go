package fsm

import (
	"slices"
	"strings"
)

// mermaidHeader opens a Mermaid state diagram. It is rendered even for an empty graph, so the output is always valid.
const mermaidHeader = "stateDiagram-v2"

// String renders the graph as one edge per line, so EventGraph satisfies fmt.Stringer.
//
// Edges are sorted by source state and then by event, so the output is stable across calls and usable as a golden
// file. An empty graph renders as the empty string.
//
// Example:
//
//	fmt.Println(orderGraph)
//	// Draft ---cancel---> Canceled
//	// Draft ---submit---> Review
//	// Review ---pay---> Paid
func (g EventGraph[S, E]) String() string {
	var b strings.Builder

	for _, from := range g.sortedStates() {
		for _, event := range g.sortedEvents(from) {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}

			to := g.edges[from][event]
			b.WriteString(string(from) + " ---" + string(event) + "---> " + string(to))
		}
	}

	return b.String()
}

// Mermaid renders the graph as a Mermaid state diagram, with each event as the edge label.
//
// The output is sorted like String's and pastes into Markdown that supports Mermaid.
//
// Example:
//
//	fmt.Println(orderGraph.Mermaid())
//	// stateDiagram-v2
//	//     Draft --> Canceled: cancel
//	//     Draft --> Review: submit
func (g EventGraph[S, E]) Mermaid() string {
	var b strings.Builder

	b.WriteString(mermaidHeader)

	for _, from := range g.sortedStates() {
		for _, event := range g.sortedEvents(from) {
			to := g.edges[from][event]
			b.WriteString("\n    " + string(from) + " --> " + string(to) + ": " + string(event))
		}
	}

	return b.String()
}

// sortedStates returns every state that has at least one outgoing edge, in order.
//
// Map iteration order is randomized, so renderers walk this instead of ranging the map directly.
func (g EventGraph[S, E]) sortedStates() []S {
	states := make([]S, 0, len(g.edges))
	for from := range g.edges {
		states = append(states, from)
	}

	slices.Sort(states)

	return states
}

// sortedEvents returns the events leaving from, in order.
func (g EventGraph[S, E]) sortedEvents(from S) []E {
	byEvent := g.edges[from]

	events := make([]E, 0, len(byEvent))
	for event := range byEvent {
		events = append(events, event)
	}

	slices.Sort(events)

	return events
}

// String renders the graph as one edge per line, so Graph satisfies fmt.Stringer.
//
// No event names appear, since this surface has none. Edges are sorted by source and then by target, so the output is
// stable across calls. An empty graph renders as the empty string.
//
// Example:
//
//	fmt.Println(statusGraph)
//	// Draft -> Canceled
//	// Draft -> Review
//	// Review -> Paid
func (g Graph[S]) String() string {
	var b strings.Builder

	for _, from := range g.inner.sortedStates() {
		// On this surface each edge's event name is its target state, so the sorted events are the sorted targets.
		for _, to := range g.inner.sortedEvents(from) {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}

			b.WriteString(string(from) + " -> " + string(to))
		}
	}

	return b.String()
}

// Mermaid renders the graph as a Mermaid state diagram, with unlabeled edges.
//
// Example:
//
//	fmt.Println(statusGraph.Mermaid())
//	// stateDiagram-v2
//	//     Draft --> Canceled
//	//     Draft --> Review
func (g Graph[S]) Mermaid() string {
	var b strings.Builder

	b.WriteString(mermaidHeader)

	for _, from := range g.inner.sortedStates() {
		for _, to := range g.inner.sortedEvents(from) {
			b.WriteString("\n    " + string(from) + " --> " + string(to))
		}
	}

	return b.String()
}
