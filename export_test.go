package fsm

// This file is compiled only during tests of this package. It exposes internals to the external fsm_test package so
// contract tests can observe a sealed graph without the public API having to grow accessors that callers do not need.

// EdgeMapForTest returns the graph's edges as from -> event -> to.
//
// The returned map is the graph's own, so tests that mutate it are testing themselves, not the package.
func (g EventGraph[S, E]) EdgeMapForTest() map[S]map[E]S {
	return g.edges
}

// TargetForTest reports the target of the edge (from, event) and whether it exists.
func (g EventGraph[S, E]) TargetForTest(from S, event E) (S, bool) {
	return g.target(from, event)
}

// EdgeCountForTest returns the total number of edges in the graph.
func (g EventGraph[S, E]) EdgeCountForTest() int {
	n := 0
	for _, byEvent := range g.edges {
		n += len(byEvent)
	}

	return n
}
