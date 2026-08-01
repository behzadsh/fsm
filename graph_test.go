package fsm_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/behzadsh/fsm"
)

func TestNewEventGraph(t *testing.T) {
	t.Run("a builder with no edges yields an empty graph", func(t *testing.T) {
		g, err := fsm.NewEventGraph[orderState, orderEvent]().Build()
		if err != nil {
			t.Fatalf("Build() returned error: %v", err)
		}

		if got := g.EdgeCountForTest(); got != 0 {
			t.Errorf("edge count = %d, want 0", got)
		}
	})

	t.Run("the builder is usable as a chain", func(t *testing.T) {
		g := fsm.NewEventGraph[orderState, orderEvent]().
			On(stateDraft, eventSubmit, stateReview).
			On(stateReview, eventPay, statePaid).
			MustBuild()

		if got := g.EdgeCountForTest(); got != 2 {
			t.Errorf("edge count = %d, want 2", got)
		}
	})
}

func TestEventGraphBuilderOn(t *testing.T) {
	type edge struct {
		from  orderState
		event orderEvent
		to    orderState
	}

	tests := []struct {
		name  string
		edges []edge
	}{
		{"a single edge", []edge{{stateDraft, eventSubmit, stateReview}}},
		{
			"two events from one state to different targets",
			[]edge{
				{stateDraft, eventSubmit, stateReview},
				{stateDraft, eventCancel, stateCanceled},
			},
		},
		{
			"two events from one state to the same target",
			[]edge{
				{stateDraft, eventSubmit, stateReview},
				{stateDraft, eventResubmit, stateReview},
			},
		},
		{
			"one event fanning out from different states to different targets",
			[]edge{
				{stateDraft, eventCancel, stateCanceled},
				{statePaid, eventCancel, stateRefunded},
			},
		},
		{
			"an edge pointing at its own source",
			[]edge{{stateReview, eventResubmit, stateReview}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := fsm.NewEventGraph[orderState, orderEvent]()
			for _, e := range tt.edges {
				b = b.On(e.from, e.event, e.to)
			}

			g, err := b.Build()
			if err != nil {
				t.Fatalf("Build() returned error: %v", err)
			}

			if got := g.EdgeCountForTest(); got != len(tt.edges) {
				t.Errorf("edge count = %d, want %d", got, len(tt.edges))
			}

			for _, e := range tt.edges {
				to, ok := g.TargetForTest(e.from, e.event)
				if !ok {
					t.Errorf("edge (%s, %s) missing", e.from, e.event)

					continue
				}

				if to != e.to {
					t.Errorf("edge (%s, %s) -> %s, want -> %s", e.from, e.event, to, e.to)
				}
			}
		})
	}
}

// Two events reaching the same target must both survive the build. Keying edges by target instead of by event would
// silently collapse them, which is why this is asserted on its own rather than only as a row in the table above.
func TestEventGraphTwoEventsSameTarget(t *testing.T) {
	g := fsm.NewEventGraph[orderState, orderEvent]().
		On(stateDraft, eventSubmit, stateReview).
		On(stateDraft, eventResubmit, stateReview).
		MustBuild()

	if got := g.EdgeCountForTest(); got != 2 {
		t.Fatalf("edge count = %d, want 2 (both events must survive)", got)
	}

	for _, event := range []orderEvent{eventSubmit, eventResubmit} {
		to, ok := g.TargetForTest(stateDraft, event)
		if !ok {
			t.Errorf("edge (%s, %s) missing", stateDraft, event)

			continue
		}

		if to != stateReview {
			t.Errorf("edge (%s, %s) -> %s, want -> %s", stateDraft, event, to, stateReview)
		}
	}
}

func TestEventGraphBuilderDuplicateEdge(t *testing.T) {
	t.Run("Build reports the conflict", func(t *testing.T) {
		_, err := fsm.NewEventGraph[orderState, orderEvent]().
			On(stateDraft, eventSubmit, stateReview).
			On(stateDraft, eventSubmit, statePaid).
			Build()
		if err == nil {
			t.Fatal("Build() returned nil error, want a duplicate-edge error")
		}

		const want = "duplicate edge (Draft, submit): -> Review and -> Paid"
		if err.Error() != want {
			t.Errorf("err = %q, want %q", err.Error(), want)
		}
	})

	t.Run("the first edge wins and the conflicting one is not applied", func(t *testing.T) {
		b := fsm.NewEventGraph[orderState, orderEvent]().
			On(stateDraft, eventSubmit, stateReview).
			On(stateDraft, eventSubmit, statePaid)

		// Build reports the error, but the partially assembled graph must not have taken the second target, or
		// "last write wins" would be the silent behavior the error exists to prevent.
		g, err := b.Build()
		if err == nil {
			t.Fatal("Build() returned nil error, want a duplicate-edge error")
		}

		if to, ok := g.TargetForTest(stateDraft, eventSubmit); ok && to != stateReview {
			t.Errorf("edge (%s, %s) -> %s, want the first target %s", stateDraft, eventSubmit, to, stateReview)
		}
	})

	t.Run("several conflicts are all reported", func(t *testing.T) {
		_, err := fsm.NewEventGraph[orderState, orderEvent]().
			On(stateDraft, eventSubmit, stateReview).
			On(stateDraft, eventSubmit, statePaid).
			On(stateReview, eventPay, statePaid).
			On(stateReview, eventPay, stateShipped).
			Build()
		if err == nil {
			t.Fatal("Build() returned nil error, want two duplicate-edge errors")
		}

		for _, want := range []string{
			"duplicate edge (Draft, submit): -> Review and -> Paid",
			"duplicate edge (Review, pay): -> Paid and -> Shipped",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("err = %q, want it to contain %q", err.Error(), want)
			}
		}
	})

	t.Run("re-declaring the identical edge is still a conflict", func(t *testing.T) {
		_, err := fsm.NewEventGraph[orderState, orderEvent]().
			On(stateDraft, eventSubmit, stateReview).
			On(stateDraft, eventSubmit, stateReview).
			Build()
		if err == nil {
			t.Fatal("Build() returned nil error, want a duplicate-edge error")
		}
	})
}

func TestEventGraphBuilderMustBuild(t *testing.T) {
	t.Run("returns the graph when there is no conflict", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustBuild() panicked on a valid graph: %v", r)
			}
		}()

		if got := orderGraph().EdgeCountForTest(); got != 7 {
			t.Errorf("edge count = %d, want 7", got)
		}
	})

	t.Run("panics on a conflict, carrying the same message Build reports", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("MustBuild() did not panic on a duplicate edge")
			}

			err, ok := r.(error)
			if !ok {
				t.Fatalf("recovered value is %T, want error", r)
			}

			const want = "duplicate edge (Draft, submit): -> Review and -> Paid"
			if err.Error() != want {
				t.Errorf("panic err = %q, want %q", err.Error(), want)
			}
		}()

		fsm.NewEventGraph[orderState, orderEvent]().
			On(stateDraft, eventSubmit, stateReview).
			On(stateDraft, eventSubmit, statePaid).
			MustBuild()
	})
}

// A built graph is sealed: later use of the builder that produced it must not change what the graph allows. The old
// implementation stored the caller's map by reference, so this is the contract that replaces that documented footgun.
func TestEventGraphSealedAfterBuild(t *testing.T) {
	b := fsm.NewEventGraph[orderState, orderEvent]().
		On(stateDraft, eventSubmit, stateReview)

	g := b.MustBuild()

	b.On(statePaid, eventShip, stateShipped)

	if got := g.EdgeCountForTest(); got != 1 {
		t.Errorf("edge count = %d, want 1; the built graph must not see later builder writes", got)
	}

	if _, ok := g.TargetForTest(statePaid, eventShip); ok {
		t.Error("edge (Paid, ship) leaked into a graph built before it was declared")
	}
}

// Two graphs built from the same builder must not share state either.
func TestEventGraphBuildTwice(t *testing.T) {
	b := fsm.NewEventGraph[orderState, orderEvent]().
		On(stateDraft, eventSubmit, stateReview)

	first := b.MustBuild()
	b.On(statePaid, eventShip, stateShipped)
	second := b.MustBuild()

	if got := first.EdgeCountForTest(); got != 1 {
		t.Errorf("first graph edge count = %d, want 1", got)
	}

	if got := second.EdgeCountForTest(); got != 2 {
		t.Errorf("second graph edge count = %d, want 2", got)
	}
}

func ExampleNewEventGraph() {
	graph := fsm.NewEventGraph[orderState, orderEvent]().
		On(stateDraft, eventSubmit, stateReview).
		On(stateDraft, eventCancel, stateCanceled).
		On(stateReview, eventPay, statePaid).
		MustBuild()

	to, ok := graph.TargetForTest(stateDraft, eventSubmit)
	fmt.Println(to, ok)

	// No edge leaves Paid in this graph, so Paid is terminal.
	_, ok = graph.TargetForTest(statePaid, eventShip)
	fmt.Println(ok)

	// Output:
	// Review true
	// false
}

func ExampleEventGraphBuilder_Build() {
	// Build returns the error instead of panicking, which is what conditional construction needs.
	builder := fsm.NewEventGraph[orderState, orderEvent]().
		On(stateDraft, eventSubmit, stateReview)

	allowResubmit := true
	if allowResubmit {
		builder = builder.On(stateDraft, eventResubmit, stateReview)
	}

	graph, err := builder.Build()
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(graph.EdgeCountForTest())

	// Output:
	// 2
}

// --- the simple, state-only surface -------------------------------------------------------------------------------

func TestNewGraph(t *testing.T) {
	t.Run("a builder with no edges yields an empty graph", func(t *testing.T) {
		if _, err := fsm.NewGraph[orderState]().Build(); err != nil {
			t.Fatalf("Build() returned error: %v", err)
		}
	})

	t.Run("To is variadic, declaring one source to many targets", func(t *testing.T) {
		g := fsm.NewGraph[orderState]().
			To(stateDraft, stateReview, stateCanceled).
			MustBuild()

		m := fsm.MustNew(g, stateDraft)

		for _, to := range []orderState{stateReview, stateCanceled} {
			if err := m.CanTransitionTo(context.Background(), to); err != nil {
				t.Errorf("CanTransitionTo(%q) = %v, want nil", to, err)
			}
		}
	})

	t.Run("To with no targets declares nothing", func(t *testing.T) {
		g := fsm.NewGraph[orderState]().To(stateDraft).MustBuild()

		if _, err := fsm.New(g, stateDraft); err == nil {
			t.Error("New = nil error, want ErrUnknownState; no edge means no state was declared")
		}
	})
}

func TestGraphBuilderDuplicateEdge(t *testing.T) {
	t.Run("Build reports the conflict without naming an event", func(t *testing.T) {
		_, err := fsm.NewGraph[orderState]().
			To(stateDraft, stateReview).
			To(stateDraft, stateReview).
			Build()
		if err == nil {
			t.Fatal("Build() returned nil error, want a duplicate-edge error")
		}

		const want = "duplicate edge Draft -> Review"
		if err.Error() != want {
			t.Errorf("err = %q, want %q", err.Error(), want)
		}

		assertNoEventVocabulary(t, err)
	})

	t.Run("a duplicate inside a single variadic call is still caught", func(t *testing.T) {
		_, err := fsm.NewGraph[orderState]().
			To(stateDraft, stateReview, stateReview).
			Build()
		if err == nil {
			t.Fatal("Build() returned nil error, want a duplicate-edge error")
		}
	})

	t.Run("MustBuild panics on the conflict", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("MustBuild() did not panic on a duplicate edge")
			}
		}()

		fsm.NewGraph[orderState]().
			To(stateDraft, stateReview).
			To(stateDraft, stateReview).
			MustBuild()
	})
}

// A graph built by the simple builder is sealed exactly as the labeled one is.
func TestGraphSealedAfterBuild(t *testing.T) {
	ctx := context.Background()

	b := fsm.NewGraph[orderState]().To(stateDraft, stateReview)
	g := b.MustBuild()

	b.To(stateReview, statePaid)

	m := fsm.MustNew(g, stateDraft)
	if err := m.TransitionTo(ctx, stateReview); err != nil {
		t.Fatalf("TransitionTo returned error: %v", err)
	}

	if err := m.CanTransitionTo(ctx, statePaid); err == nil {
		t.Error("an edge declared after the build leaked into the graph")
	}
}

func ExampleNewGraph() {
	ctx := context.Background()

	graph := fsm.NewGraph[orderState]().
		To(stateDraft, stateReview, stateCanceled).
		To(stateReview, statePaid).
		MustBuild()

	m := fsm.MustNew(graph, stateDraft)

	if err := m.TransitionTo(ctx, stateReview); err != nil {
		fmt.Println(err)
	}

	fmt.Println(m.Current())

	// No edge leads from Review to Shipped.
	if err := m.TransitionTo(ctx, stateShipped); err != nil {
		fmt.Println(err)
	}

	// Output:
	// Review
	// fsm: cannot transition Review -> Shipped: invalid transition
}
