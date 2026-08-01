package fsm_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/behzadsh/fsm"
)

func TestEventGraphString(t *testing.T) {
	got := orderGraph().String()

	want := strings.Join([]string{
		"Draft ---cancel---> Canceled",
		"Draft ---resubmit---> Review",
		"Draft ---submit---> Review",
		"Paid ---cancel---> Refunded",
		"Paid ---ship---> Shipped",
		"Review ---cancel---> Canceled",
		"Review ---pay---> Paid",
	}, "\n")

	if got != want {
		t.Errorf("String() =\n%s\n\nwant\n%s", got, want)
	}
}

func TestEventGraphMermaid(t *testing.T) {
	got := orderGraph().Mermaid()

	want := strings.Join([]string{
		"stateDiagram-v2",
		"    Draft --> Canceled: cancel",
		"    Draft --> Review: resubmit",
		"    Draft --> Review: submit",
		"    Paid --> Refunded: cancel",
		"    Paid --> Shipped: ship",
		"    Review --> Canceled: cancel",
		"    Review --> Paid: pay",
	}, "\n")

	if got != want {
		t.Errorf("Mermaid() =\n%s\n\nwant\n%s", got, want)
	}
}

func TestGraphString(t *testing.T) {
	got := statusGraph().String()

	want := strings.Join([]string{
		"Draft -> Canceled",
		"Draft -> Review",
		"Paid -> Refunded",
		"Paid -> Shipped",
		"Review -> Canceled",
		"Review -> Paid",
	}, "\n")

	if got != want {
		t.Errorf("String() =\n%s\n\nwant\n%s", got, want)
	}
}

func TestGraphMermaid(t *testing.T) {
	got := statusGraph().Mermaid()

	want := strings.Join([]string{
		"stateDiagram-v2",
		"    Draft --> Canceled",
		"    Draft --> Review",
		"    Paid --> Refunded",
		"    Paid --> Shipped",
		"    Review --> Canceled",
		"    Review --> Paid",
	}, "\n")

	if got != want {
		t.Errorf("Mermaid() =\n%s\n\nwant\n%s", got, want)
	}
}

// Go randomizes map iteration order per range, so rendering the same graph repeatedly checks that the output is
// sorted rather than stable by accident.
func TestRenderersAreDeterministic(t *testing.T) {
	event := orderGraph()
	state := statusGraph()

	renderers := map[string]func() string{
		"EventGraph.String":  event.String,
		"EventGraph.Mermaid": event.Mermaid,
		"Graph.String":       state.String,
		"Graph.Mermaid":      state.Mermaid,
	}

	for name, render := range renderers {
		t.Run(name, func(t *testing.T) {
			first := render()

			for i := 0; i < 50; i++ {
				if got := render(); got != first {
					t.Fatalf("call %d differs from the first:\n%s\n\nwant\n%s", i+2, got, first)
				}
			}
		})
	}
}

// Every edge's event name on the simple surface is its target state, so an event name reaching the output would show
// up as a duplicated state on the line.
func TestSimpleRenderersEmitNoEventLabels(t *testing.T) {
	for name, got := range map[string]string{
		"String":  statusGraph().String(),
		"Mermaid": statusGraph().Mermaid(),
	} {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(got, ":") && name == "Mermaid" {
				t.Errorf("%s emitted a label:\n%s", name, got)
			}

			if strings.Contains(got, "--") && name == "String" {
				t.Errorf("%s emitted an event arrow:\n%s", name, got)
			}
		})
	}
}

func TestRenderEmptyGraph(t *testing.T) {
	t.Run("an empty EventGraph renders nothing but the header", func(t *testing.T) {
		empty := fsm.NewEventGraph[orderState, orderEvent]().MustBuild()

		if got := empty.String(); got != "" {
			t.Errorf("String() = %q, want empty", got)
		}

		if got := empty.Mermaid(); got != "stateDiagram-v2" {
			t.Errorf("Mermaid() = %q, want just the header", got)
		}
	})

	t.Run("an empty Graph renders nothing but the header", func(t *testing.T) {
		empty := fsm.NewGraph[orderState]().MustBuild()

		if got := empty.String(); got != "" {
			t.Errorf("String() = %q, want empty", got)
		}

		if got := empty.Mermaid(); got != "stateDiagram-v2" {
			t.Errorf("Mermaid() = %q, want just the header", got)
		}
	})
}

// Both graph types satisfy fmt.Stringer.
func TestGraphsSatisfyStringer(t *testing.T) {
	var event fmt.Stringer = orderGraph()
	if !strings.Contains(event.String(), "---submit--->") {
		t.Errorf("EventGraph as fmt.Stringer = %q", event.String())
	}

	var state fmt.Stringer = statusGraph()
	if !strings.Contains(state.String(), "Draft -> Review") {
		t.Errorf("Graph as fmt.Stringer = %q", state.String())
	}
}

func ExampleEventGraph_Mermaid() {
	graph := fsm.NewEventGraph[orderState, orderEvent]().
		On(stateDraft, eventSubmit, stateReview).
		On(stateDraft, eventCancel, stateCanceled).
		On(stateReview, eventPay, statePaid).
		MustBuild()

	fmt.Println(graph.Mermaid())

	// Output:
	// stateDiagram-v2
	//     Draft --> Canceled: cancel
	//     Draft --> Review: submit
	//     Review --> Paid: pay
}

func ExampleGraph_String() {
	graph := fsm.NewGraph[orderState]().
		To(stateDraft, stateReview, stateCanceled).
		To(stateReview, statePaid).
		MustBuild()

	fmt.Println(graph)

	// Output:
	// Draft -> Canceled
	// Draft -> Review
	// Review -> Paid
}
