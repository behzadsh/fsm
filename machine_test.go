package fsm_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/behzadsh/fsm"
)

func TestNewEventMachine(t *testing.T) {
	tests := []struct {
		name    string
		initial orderState
		wantErr bool
	}{
		{"a state with outgoing edges", stateDraft, false},
		{"a terminal state, reachable but with no outgoing edge", stateShipped, false},
		{"another terminal state", stateCanceled, false},
		{"a state absent from the graph", stateUnknown, true},
		{"the empty state", orderState(""), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := fsm.NewEventMachine(orderGraph(), tt.initial)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewEventMachine(%q) returned nil error, want ErrUnknownState", tt.initial)
				}

				if !errors.Is(err, fsm.ErrUnknownState) {
					t.Errorf("err = %v, want it to wrap ErrUnknownState", err)
				}

				if m != nil {
					t.Error("machine is non-nil alongside an error, want nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("NewEventMachine(%q) returned error: %v", tt.initial, err)
			}

			if got := m.Current(); got != tt.initial {
				t.Errorf("Current() = %q, want %q", got, tt.initial)
			}
		})
	}
}

// Constructing against an empty graph must fail for every state, since an empty graph declares no states at all.
func TestNewEventMachineEmptyGraph(t *testing.T) {
	empty := fsm.NewEventGraph[orderState, orderEvent]().MustBuild()

	if _, err := fsm.NewEventMachine(empty, stateDraft); !errors.Is(err, fsm.ErrUnknownState) {
		t.Errorf("err = %v, want ErrUnknownState", err)
	}
}

func TestMustEventMachine(t *testing.T) {
	t.Run("returns a machine for a known state", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustEventMachine panicked on a valid state: %v", r)
			}
		}()

		if got := fsm.MustEventMachine(orderGraph(), stateDraft).Current(); got != stateDraft {
			t.Errorf("Current() = %q, want %q", got, stateDraft)
		}
	})

	t.Run("panics on a state absent from the graph", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("MustEventMachine did not panic on an unknown state")
			}

			err, ok := r.(error)
			if !ok {
				t.Fatalf("recovered value is %T, want error", r)
			}

			if !errors.Is(err, fsm.ErrUnknownState) {
				t.Errorf("panic err = %v, want it to wrap ErrUnknownState", err)
			}
		}()

		fsm.MustEventMachine(orderGraph(), stateUnknown)
	})
}

func TestEventMachineCurrentIsString(t *testing.T) {
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	t.Run("Current reports the state", func(t *testing.T) {
		if got := m.Current(); got != stateDraft {
			t.Errorf("Current() = %q, want %q", got, stateDraft)
		}
	})

	t.Run("Is compares without consulting the graph", func(t *testing.T) {
		if !m.Is(stateDraft) {
			t.Error("Is(Draft) = false, want true")
		}

		if m.Is(stateReview) {
			t.Error("Is(Review) = true, want false")
		}

		if m.Is(stateUnknown) {
			t.Error("Is(Unknown) = true, want false")
		}
	})

	t.Run("String satisfies fmt.Stringer", func(t *testing.T) {
		if got := m.String(); got != "Draft" {
			t.Errorf("String() = %q, want %q", got, "Draft")
		}

		var stringer fmt.Stringer = m
		if got := stringer.String(); got != "Draft" {
			t.Errorf("as fmt.Stringer, String() = %q, want %q", got, "Draft")
		}
	})
}

func TestEventMachineFire(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		from    orderState
		event   orderEvent
		want    orderState
		wantErr string
	}{
		{"a declared edge", stateDraft, eventSubmit, stateReview, ""},
		{"a second event reaching the same target", stateDraft, eventResubmit, stateReview, ""},
		{"into a terminal state", statePaid, eventShip, stateShipped, ""},
		{"the same event fanning out from another source", statePaid, eventCancel, stateRefunded, ""},
		{
			"an event with no edge from here",
			stateDraft,
			eventPay,
			stateDraft,
			"fsm: cannot fire pay from Draft: invalid transition",
		},
		{
			"an event absent from the whole graph",
			stateDraft,
			eventUnknown,
			stateDraft,
			"fsm: cannot fire unknown from Draft: invalid transition",
		},
		{
			"from a terminal state nothing leaves",
			stateShipped,
			eventCancel,
			stateShipped,
			"fsm: cannot fire cancel from Shipped: invalid transition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := fsm.MustEventMachine(orderGraph(), tt.from)

			err := m.Fire(ctx, tt.event)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Fire(%q) returned error: %v", tt.event, err)
				}
			} else {
				if err == nil {
					t.Fatalf("Fire(%q) returned nil error, want %q", tt.event, tt.wantErr)
				}

				if err.Error() != tt.wantErr {
					t.Errorf("err = %q, want %q", err.Error(), tt.wantErr)
				}

				if !errors.Is(err, fsm.ErrInvalidTransition) {
					t.Errorf("err = %v, want it to wrap ErrInvalidTransition", err)
				}
			}

			if got := m.Current(); got != tt.want {
				t.Errorf("Current() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A rejected Fire must leave the machine exactly where it was, and must not prevent a later legal move.
func TestEventMachineFireRejectionIsInert(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	if err := m.Fire(ctx, eventPay); err == nil {
		t.Fatal("Fire(pay) from Draft returned nil error, want a rejection")
	}

	if got := m.Current(); got != stateDraft {
		t.Fatalf("Current() = %q after a rejected Fire, want %q", got, stateDraft)
	}

	if err := m.Fire(ctx, eventSubmit); err != nil {
		t.Fatalf("a rejected Fire blocked a later legal one: %v", err)
	}

	if got := m.Current(); got != stateReview {
		t.Errorf("Current() = %q, want %q", got, stateReview)
	}
}

// A failed resolve carries PhaseResolve and no target, since no edge was ever found.
func TestEventMachineFireErrorShape(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	err := m.Fire(ctx, eventPay)

	var te *fsm.TransitionError[orderState, orderEvent]
	if !errors.As(err, &te) {
		t.Fatalf("err is %T, want *fsm.TransitionError", err)
	}

	if te.Phase != fsm.PhaseResolve {
		t.Errorf("Phase = %v, want PhaseResolve", te.Phase)
	}

	if te.From != stateDraft {
		t.Errorf("From = %q, want %q", te.From, stateDraft)
	}

	if te.Event != eventPay {
		t.Errorf("Event = %q, want %q", te.Event, eventPay)
	}

	var zero orderState
	if te.To != zero {
		t.Errorf("To = %q, want the zero value; resolve found no target to name", te.To)
	}
}

func TestEventMachineCanFire(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		from    orderState
		event   orderEvent
		wantErr bool
	}{
		{"a declared edge", stateDraft, eventSubmit, false},
		{"another declared edge", stateDraft, eventCancel, false},
		{"no edge for this event from here", stateDraft, eventPay, true},
		{"an event absent from the whole graph", stateDraft, eventUnknown, true},
		{"from a terminal state", stateCanceled, eventSubmit, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := fsm.MustEventMachine(orderGraph(), tt.from)

			err := m.CanFire(ctx, tt.event)

			if tt.wantErr && err == nil {
				t.Errorf("CanFire(%q) = nil, want an error", tt.event)
			}

			if !tt.wantErr && err != nil {
				t.Errorf("CanFire(%q) = %v, want nil", tt.event, err)
			}

			// CanFire must never move the machine, whatever it reports.
			if got := m.Current(); got != tt.from {
				t.Errorf("Current() = %q after CanFire, want %q", got, tt.from)
			}
		})
	}
}

func TestEventMachineForceState(t *testing.T) {
	tests := []struct {
		name    string
		from    orderState
		to      orderState
		wantErr bool
	}{
		{"to a state with no edge from here", stateDraft, stateShipped, false},
		{"backwards against every declared edge", stateShipped, stateDraft, false},
		{"out of a terminal state", stateCanceled, stateDraft, false},
		{"to the state it is already in", stateDraft, stateDraft, false},
		{"to a state absent from the graph", stateDraft, stateUnknown, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := fsm.MustEventMachine(orderGraph(), tt.from)

			err := m.ForceState(tt.to)

			if tt.wantErr {
				if !errors.Is(err, fsm.ErrUnknownState) {
					t.Errorf("err = %v, want it to wrap ErrUnknownState", err)
				}

				if got := m.Current(); got != tt.from {
					t.Errorf("Current() = %q after a rejected ForceState, want %q", got, tt.from)
				}

				return
			}

			if err != nil {
				t.Fatalf("ForceState(%q) returned error: %v", tt.to, err)
			}

			if got := m.Current(); got != tt.to {
				t.Errorf("Current() = %q, want %q", got, tt.to)
			}
		})
	}
}

func ExampleNewEventMachine() {
	m, err := fsm.NewEventMachine(orderGraph(), stateDraft)
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println(m.Current())

	// The initial state is validated, which matters when it came out of a database column.
	if _, err := fsm.NewEventMachine(orderGraph(), stateUnknown); err != nil {
		fmt.Println(err)
	}

	// Output:
	// Draft
	// fsm: initial state Unknown: unknown state
}

func ExampleEventMachine_Fire() {
	ctx := context.Background()
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	if err := m.Fire(ctx, eventSubmit); err != nil {
		fmt.Println(err)
	}

	fmt.Println(m.Current())

	// No edge leaves Review on ship, so the machine stays put.
	if err := m.Fire(ctx, eventShip); err != nil {
		fmt.Println(err)
	}

	fmt.Println(m.Current())

	// Output:
	// Review
	// fsm: cannot fire ship from Review: invalid transition
	// Review
}

func ExampleEventMachine_ForceState() {
	m := fsm.MustEventMachine(orderGraph(), stateShipped)

	// ForceState ignores the graph entirely. It exists for operational repair, not for ordinary application code.
	if err := m.ForceState(stateDraft); err != nil {
		fmt.Println(err)
	}

	fmt.Println(m.Current())

	// It still refuses a state the graph never declared.
	if err := m.ForceState(stateUnknown); err != nil {
		fmt.Println(err)
	}

	// Output:
	// Draft
	// fsm: force state Unknown: unknown state
}
