package fsm_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// Constructing against an empty graph fails for every state, since an empty graph declares no states.
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

// A rejected Fire leaves the machine where it was and does not prevent a later allowed move.
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

// --- guards -------------------------------------------------------------------------------------------------------

func TestEventMachineGuard(t *testing.T) {
	ctx := context.Background()
	denied := errors.New("not authorized")

	t.Run("a passing guard lets the transition through", func(t *testing.T) {
		m := fsm.MustEventMachine(orderGraph(), stateDraft)
		m.Guard(stateDraft, eventSubmit, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
			return nil
		})

		if err := m.Fire(ctx, eventSubmit); err != nil {
			t.Fatalf("Fire returned error: %v", err)
		}

		if got := m.Current(); got != stateReview {
			t.Errorf("Current() = %q, want %q", got, stateReview)
		}
	})

	t.Run("a refusing guard blocks it and nothing moves", func(t *testing.T) {
		m := fsm.MustEventMachine(orderGraph(), stateDraft)
		m.Guard(stateDraft, eventSubmit, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
			return denied
		})

		err := m.Fire(ctx, eventSubmit)
		if err == nil {
			t.Fatal("Fire returned nil error, want the guard's refusal")
		}

		if !errors.Is(err, denied) {
			t.Errorf("err = %v, want it to wrap the guard's error", err)
		}

		var te *fsm.TransitionError[orderState, orderEvent]
		if !errors.As(err, &te) {
			t.Fatalf("err is %T, want *fsm.TransitionError", err)
		}

		if te.Phase != fsm.PhaseGuard {
			t.Errorf("Phase = %v, want PhaseGuard", te.Phase)
		}

		if te.Moved() {
			t.Error("Moved() = true, want false; a guard refuses before the commit")
		}

		if got := m.Current(); got != stateDraft {
			t.Errorf("Current() = %q, want %q", got, stateDraft)
		}
	})
}

// A guard receives the whole transition, not only the state it was registered against.
func TestEventMachineGuardReceivesTransition(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	var seen fsm.Transition[orderState, orderEvent]
	m.Guard(stateDraft, eventSubmit, func(_ context.Context, tr fsm.Transition[orderState, orderEvent]) error {
		seen = tr

		return nil
	})

	if err := m.Fire(ctx, eventSubmit); err != nil {
		t.Fatalf("Fire returned error: %v", err)
	}

	if seen.From != stateDraft || seen.To != stateReview || seen.Event != eventSubmit {
		t.Errorf("guard saw %+v, want Draft -> Review via submit", seen)
	}
}

// The context passed to Fire reaches the guard, so it can read deadlines and request-scoped values.
func TestEventMachineGuardReceivesContext(t *testing.T) {
	type ctxKey string

	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	var got any
	m.Guard(stateDraft, eventSubmit, func(c context.Context, _ fsm.Transition[orderState, orderEvent]) error {
		got = c.Value(ctxKey("k"))

		return nil
	})

	ctx := context.WithValue(context.Background(), ctxKey("k"), "v")
	if err := m.Fire(ctx, eventSubmit); err != nil {
		t.Fatalf("Fire returned error: %v", err)
	}

	if got != "v" {
		t.Errorf("guard saw ctx value %v, want \"v\"", got)
	}
}

// Guards key on the edge (from, event), so two events reaching the same target do not share one.
func TestEventMachineGuardKeying(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	m.Guard(stateDraft, eventSubmit, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		return errors.New("submit blocked")
	})

	if err := m.CanFire(ctx, eventSubmit); err == nil {
		t.Error("CanFire(submit) = nil, want the guard's refusal")
	}

	if err := m.CanFire(ctx, eventResubmit); err != nil {
		t.Errorf("CanFire(resubmit) = %v, want nil; it must not inherit submit's guard", err)
	}

	if err := m.CanFire(ctx, eventCancel); err != nil {
		t.Errorf("CanFire(cancel) = %v, want nil; an unrelated edge must be unguarded", err)
	}
}

// CanFire calls the guard. Hooks are not called.
func TestEventMachineCanFireRunsGuard(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	calls := 0
	m.Guard(stateDraft, eventSubmit, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		calls++

		return errors.New("no")
	})

	if err := m.CanFire(ctx, eventSubmit); err == nil {
		t.Fatal("CanFire = nil, want the guard's refusal")
	}

	if calls != 1 {
		t.Errorf("guard ran %d times during CanFire, want 1", calls)
	}

	if got := m.Current(); got != stateDraft {
		t.Errorf("CanFire moved the machine to %q", got)
	}
}

// Fire must call the guard once, not once directly and again through CanFire.
func TestEventMachineFireRunsGuardOnce(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	calls := 0
	m.Guard(stateDraft, eventSubmit, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		calls++

		return nil
	})

	if err := m.Fire(ctx, eventSubmit); err != nil {
		t.Fatalf("Fire returned error: %v", err)
	}

	if calls != 1 {
		t.Errorf("guard ran %d times during one Fire, want 1", calls)
	}
}

func TestEventMachineGuardOverwrite(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	m.Guard(stateDraft, eventSubmit, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		return errors.New("first")
	})
	m.Guard(stateDraft, eventSubmit, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		return errors.New("second")
	})

	err := m.CanFire(ctx, eventSubmit)
	if err == nil {
		t.Fatal("CanFire = nil, want the second guard's refusal")
	}

	if !strings.Contains(err.Error(), "second") {
		t.Errorf("err = %q, want the second registration to have replaced the first", err.Error())
	}
}

// --- exit and enter hooks -----------------------------------------------------------------------------------------

// A blocking exit hook aborts before the state changes, so nothing moved and the enter hook did not run.
func TestEventMachineOnExitBlocking(t *testing.T) {
	ctx := context.Background()
	held := errors.New("hold not released")

	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	entered := false
	m.OnExitBlocking(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		return held
	}).OnEnter(stateReview, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		entered = true

		return nil
	})

	err := m.Fire(ctx, eventSubmit)
	if err == nil {
		t.Fatal("Fire returned nil error, want the exit hook's abort")
	}

	if !errors.Is(err, held) {
		t.Errorf("err = %v, want it to wrap the hook's error", err)
	}

	var te *fsm.TransitionError[orderState, orderEvent]
	if !errors.As(err, &te) {
		t.Fatalf("err is %T, want *fsm.TransitionError", err)
	}

	if te.Phase != fsm.PhaseExit {
		t.Errorf("Phase = %v, want PhaseExit", te.Phase)
	}

	if te.Moved() {
		t.Error("Moved() = true, want false; a blocking exit hook aborts before the commit")
	}

	if got := m.Current(); got != stateDraft {
		t.Errorf("Current() = %q, want %q", got, stateDraft)
	}

	if entered {
		t.Error("the enter hook ran even though the transition was aborted")
	}
}

// A reporting exit hook returns its error without stopping the transition, so the state changes.
func TestEventMachineOnExitReporting(t *testing.T) {
	ctx := context.Background()
	metrics := errors.New("metrics push failed")

	m := fsm.MustEventMachine(orderGraph(), stateDraft)
	m.OnExit(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		return metrics
	})

	err := m.Fire(ctx, eventSubmit)
	if err == nil {
		t.Fatal("Fire returned nil error, want the hook's error reported")
	}

	if !errors.Is(err, metrics) {
		t.Errorf("err = %v, want it to wrap the hook's error", err)
	}

	if got := m.Current(); got != stateReview {
		t.Fatalf("Current() = %q, want %q; a non-blocking hook must not stop the move", got, stateReview)
	}

	var te *fsm.TransitionError[orderState, orderEvent]
	if !errors.As(err, &te) {
		t.Fatalf("err is %T, want *fsm.TransitionError", err)
	}

	if !te.Moved() {
		t.Error("Moved() = false, want true; the machine did move despite the reported error")
	}
}

func TestEventMachineOnEnter(t *testing.T) {
	ctx := context.Background()
	notify := errors.New("notify failed")

	m := fsm.MustEventMachine(orderGraph(), stateDraft)
	m.OnEnter(stateReview, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		return notify
	})

	err := m.Fire(ctx, eventSubmit)
	if err == nil {
		t.Fatal("Fire returned nil error, want the enter hook's error")
	}

	if got := m.Current(); got != stateReview {
		t.Fatalf("Current() = %q, want %q; an enter hook runs after the commit", got, stateReview)
	}

	var te *fsm.TransitionError[orderState, orderEvent]
	if !errors.As(err, &te) {
		t.Fatalf("err is %T, want *fsm.TransitionError", err)
	}

	if te.Phase != fsm.PhaseEnter {
		t.Errorf("Phase = %v, want PhaseEnter", te.Phase)
	}

	if !te.Moved() {
		t.Error("Moved() = false, want true")
	}
}

// OnExit and OnExitBlocking share one slot, so the last registration sets both the function and whether it blocks.
func TestEventMachineExitHookSlotIsShared(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")

	t.Run("OnExit after OnExitBlocking stops blocking", func(t *testing.T) {
		m := fsm.MustEventMachine(orderGraph(), stateDraft)

		m.OnExitBlocking(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
			return boom
		})
		m.OnExit(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
			return boom
		})

		if err := m.Fire(ctx, eventSubmit); err == nil {
			t.Fatal("Fire returned nil error, want the reported hook error")
		}

		if got := m.Current(); got != stateReview {
			t.Errorf("Current() = %q, want %q; the later OnExit must not block", got, stateReview)
		}
	})

	t.Run("OnExitBlocking after OnExit starts blocking", func(t *testing.T) {
		m := fsm.MustEventMachine(orderGraph(), stateDraft)

		m.OnExit(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
			return boom
		})
		m.OnExitBlocking(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
			return boom
		})

		if err := m.Fire(ctx, eventSubmit); err == nil {
			t.Fatal("Fire returned nil error, want the abort")
		}

		if got := m.Current(); got != stateDraft {
			t.Errorf("Current() = %q, want %q; the later OnExitBlocking must abort", got, stateDraft)
		}
	})

	t.Run("only one exit hook runs, the last registered", func(t *testing.T) {
		m := fsm.MustEventMachine(orderGraph(), stateDraft)

		var ran []string
		m.OnExit(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
			ran = append(ran, "first")

			return nil
		})
		m.OnExit(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
			ran = append(ran, "second")

			return nil
		})

		if err := m.Fire(ctx, eventSubmit); err != nil {
			t.Fatalf("Fire returned error: %v", err)
		}

		if len(ran) != 1 || ran[0] != "second" {
			t.Errorf("hooks ran = %v, want [second]; registration overwrites silently", ran)
		}
	})
}

func TestEventMachineOnEnterOverwrite(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	var ran []string
	m.OnEnter(stateReview, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		ran = append(ran, "first")

		return nil
	})
	m.OnEnter(stateReview, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		ran = append(ran, "second")

		return nil
	})

	if err := m.Fire(ctx, eventSubmit); err != nil {
		t.Fatalf("Fire returned error: %v", err)
	}

	if len(ran) != 1 || ran[0] != "second" {
		t.Errorf("hooks ran = %v, want [second]", ran)
	}
}

// The stages run in one order, with the state change between the exit and enter hooks.
func TestEventMachinePipelineOrder(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	var order []string
	m.Guard(stateDraft, eventSubmit, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		order = append(order, "guard:"+string(m.Current()))

		return nil
	}).OnExit(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		order = append(order, "exit:"+string(m.Current()))

		return nil
	}).OnEnter(stateReview, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		order = append(order, "enter:"+string(m.Current()))

		return nil
	})

	if err := m.Fire(ctx, eventSubmit); err != nil {
		t.Fatalf("Fire returned error: %v", err)
	}

	want := []string{"guard:Draft", "exit:Draft", "enter:Review"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}

	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order = %v, want %v", order, want)

			break
		}
	}
}

// A reporting exit hook and a failing enter hook can both produce errors in one call. Both must reach the caller.
func TestEventMachineBothHooksReportErrors(t *testing.T) {
	ctx := context.Background()
	exitErr := errors.New("metrics push failed")
	enterErr := errors.New("notify failed")

	m := fsm.MustEventMachine(orderGraph(), stateDraft)
	m.OnExit(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		return exitErr
	}).OnEnter(stateReview, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		return enterErr
	})

	err := m.Fire(ctx, eventSubmit)
	if err == nil {
		t.Fatal("Fire returned nil error, want both hook errors")
	}

	if !errors.Is(err, exitErr) {
		t.Error("the exit hook's error did not reach the caller")
	}

	if !errors.Is(err, enterErr) {
		t.Error("the enter hook's error did not reach the caller")
	}

	if got := m.Current(); got != stateReview {
		t.Errorf("Current() = %q, want %q", got, stateReview)
	}
}

// CanFire reports that a move is allowed, not that it will succeed.
func TestEventMachineCanFireDoesNotPromiseSuccess(t *testing.T) {
	ctx := context.Background()

	m := fsm.MustEventMachine(orderGraph(), stateDraft)
	m.OnExitBlocking(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		return errors.New("hold not released")
	})

	if err := m.CanFire(ctx, eventSubmit); err != nil {
		t.Fatalf("CanFire = %v, want nil; the edge exists and no guard refuses", err)
	}

	if err := m.Fire(ctx, eventSubmit); err == nil {
		t.Fatal("Fire = nil, want the blocking exit hook to abort a move CanFire permitted")
	}

	if got := m.Current(); got != stateDraft {
		t.Errorf("Current() = %q, want %q", got, stateDraft)
	}
}

// --- reentrancy ---------------------------------------------------------------------------------------------------

// A hook that calls back into its own machine is refused from every phase. Without this, a nested call from an exit
// hook would change the state and then be overwritten by the outer call, which resolved its target earlier.
func TestEventMachineReentrancy(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		// register wires a hook that attempts a nested Fire, and returns a pointer to where the nested error lands.
		register func(m *fsm.EventMachine[orderState, orderEvent], nested *error)
		// wantCurrent is where the machine ends up after the outer Fire.
		wantCurrent orderState
	}{
		{
			"from a guard",
			func(m *fsm.EventMachine[orderState, orderEvent], nested *error) {
				m.Guard(stateDraft, eventSubmit, func(c context.Context, _ fsm.Transition[orderState, orderEvent]) error {
					*nested = m.Fire(c, eventCancel)

					return nil
				})
			},
			stateReview,
		},
		{
			"from an exit hook",
			func(m *fsm.EventMachine[orderState, orderEvent], nested *error) {
				m.OnExit(stateDraft, func(c context.Context, _ fsm.Transition[orderState, orderEvent]) error {
					*nested = m.Fire(c, eventCancel)

					return nil
				})
			},
			stateReview,
		},
		{
			"from an enter hook",
			func(m *fsm.EventMachine[orderState, orderEvent], nested *error) {
				m.OnEnter(stateReview, func(c context.Context, _ fsm.Transition[orderState, orderEvent]) error {
					*nested = m.Fire(c, eventPay)

					return nil
				})
			},
			stateReview,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := fsm.MustEventMachine(orderGraph(), stateDraft)

			var nested error
			tt.register(m, &nested)

			if err := m.Fire(ctx, eventSubmit); err != nil {
				t.Fatalf("the outer Fire returned error: %v", err)
			}

			if !errors.Is(nested, fsm.ErrReentrant) {
				t.Errorf("nested Fire returned %v, want ErrReentrant", nested)
			}

			if got := m.Current(); got != tt.wantCurrent {
				t.Errorf("Current() = %q, want %q; the outer transition must be unaffected", got, tt.wantCurrent)
			}

			// The in-flight flag must be clear again, or the machine would be permanently stuck.
			if err := m.CanFire(ctx, eventPay); err != nil && errors.Is(err, fsm.ErrReentrant) {
				t.Error("the machine is still marked in-flight after the outer transition finished")
			}
		})
	}
}

// After any transition, including one a guard or a blocking exit hook aborted, the machine must accept a later call.
// The in-flight flag has to be cleared on every path, not only the successful one.
func TestEventMachineInFlightClearedOnEveryPath(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")

	tests := []struct {
		name  string
		setup func(m *fsm.EventMachine[orderState, orderEvent])
		event orderEvent
	}{
		{"after a failed resolve", func(*fsm.EventMachine[orderState, orderEvent]) {}, eventPay},
		{
			"after a guard refusal",
			func(m *fsm.EventMachine[orderState, orderEvent]) {
				m.Guard(stateDraft, eventSubmit, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
					return boom
				})
			},
			eventSubmit,
		},
		{
			"after a blocking exit hook aborted",
			func(m *fsm.EventMachine[orderState, orderEvent]) {
				m.OnExitBlocking(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
					return boom
				})
			},
			eventSubmit,
		},
		{
			"after an enter hook failed",
			func(m *fsm.EventMachine[orderState, orderEvent]) {
				m.OnEnter(stateCanceled, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
					return boom
				})
			},
			eventCancel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := fsm.MustEventMachine(orderGraph(), stateDraft)
			tt.setup(m)

			// The first call is expected to fail in most rows; that is the point.
			if err := m.Fire(ctx, tt.event); err != nil {
				t.Logf("first call failed as expected: %v", err)
			}

			if err := m.CanFire(ctx, eventSubmit); errors.Is(err, fsm.ErrReentrant) {
				t.Error("the machine is still marked in-flight, so it would reject every later call")
			}
		})
	}
}

// A hook may read the machine. Only moving it is refused.
func TestEventMachineReadsAreAllowedFromHooks(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	var seenDuringExit orderState
	m.OnExit(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		seenDuringExit = m.Current()

		return nil
	})

	var seenDuringEnter orderState
	m.OnEnter(stateReview, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		seenDuringEnter = m.Current()

		return nil
	})

	if err := m.Fire(ctx, eventSubmit); err != nil {
		t.Fatalf("Fire returned error: %v", err)
	}

	if seenDuringExit != stateDraft {
		t.Errorf("during the exit hook Current() = %q, want %q; the commit has not happened yet", seenDuringExit, stateDraft)
	}

	if seenDuringEnter != stateReview {
		t.Errorf("during the enter hook Current() = %q, want %q; the commit has happened", seenDuringEnter, stateReview)
	}
}

func ExampleEventMachine_OnExitBlocking() {
	ctx := context.Background()
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	// Work that can fail and whose failure should prevent the move belongs here, so the transition never commits and
	// there is nothing to undo.
	m.OnExitBlocking(stateDraft, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		return errors.New("hold not released")
	})

	if err := m.Fire(ctx, eventSubmit); err != nil {
		fmt.Println(err)
	}

	fmt.Println(m.Current())

	// Output:
	// fsm: exit Draft -> Review (event submit): hold not released
	// Draft
}

func ExampleEventMachine_Guard() {
	ctx := context.Background()
	m := fsm.MustEventMachine(orderGraph(), stateDraft)

	// A guard is a pure predicate, so CanFire may ask it without the move happening.
	m.Guard(stateDraft, eventSubmit, func(context.Context, fsm.Transition[orderState, orderEvent]) error {
		return errors.New("order has no items")
	})

	fmt.Println(m.CanFire(ctx, eventSubmit))
	fmt.Println(m.Current())

	// Output:
	// fsm: guard Draft -> Review (event submit): order has no items
	// Draft
}

// --- the simple, state-only surface -------------------------------------------------------------------------------

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		initial orderState
		wantErr bool
	}{
		{"a state with outgoing edges", stateDraft, false},
		{"a terminal state", stateShipped, false},
		{"a state absent from the graph", stateUnknown, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := fsm.New(statusGraph(), tt.initial)

			if tt.wantErr {
				if !errors.Is(err, fsm.ErrUnknownState) {
					t.Fatalf("err = %v, want it to wrap ErrUnknownState", err)
				}

				assertNoEventVocabulary(t, err)

				return
			}

			if err != nil {
				t.Fatalf("New(%q) returned error: %v", tt.initial, err)
			}

			if got := m.Current(); got != tt.initial {
				t.Errorf("Current() = %q, want %q", got, tt.initial)
			}
		})
	}
}

func TestMustNew(t *testing.T) {
	t.Run("returns a machine for a known state", func(t *testing.T) {
		if got := fsm.MustNew(statusGraph(), stateDraft).Current(); got != stateDraft {
			t.Errorf("Current() = %q, want %q", got, stateDraft)
		}
	})

	t.Run("panics on an unknown state", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("MustNew did not panic on an unknown state")
			}
		}()

		fsm.MustNew(statusGraph(), stateUnknown)
	})
}

func TestMachineTransitionTo(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		from    orderState
		to      orderState
		want    orderState
		wantErr string
	}{
		{"a declared edge", stateDraft, stateReview, stateReview, ""},
		{"a second target from the same source", stateDraft, stateCanceled, stateCanceled, ""},
		{"into a terminal state", statePaid, stateShipped, stateShipped, ""},
		{
			"no edge between these states",
			stateDraft,
			statePaid,
			stateDraft,
			"fsm: cannot transition Draft -> Paid: invalid transition",
		},
		{
			"backwards along a declared edge",
			stateReview,
			stateDraft,
			stateReview,
			"fsm: cannot transition Review -> Draft: invalid transition",
		},
		{
			"a state absent from the graph",
			stateDraft,
			stateUnknown,
			stateDraft,
			"fsm: cannot transition Draft -> Unknown: invalid transition",
		},
		{
			"from a terminal state",
			stateShipped,
			stateDraft,
			stateShipped,
			"fsm: cannot transition Shipped -> Draft: invalid transition",
		},
		{
			"a self transition that was never declared",
			stateDraft,
			stateDraft,
			stateDraft,
			"fsm: cannot transition Draft -> Draft: invalid transition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := fsm.MustNew(statusGraph(), tt.from)

			err := m.TransitionTo(ctx, tt.to)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("TransitionTo(%q) returned error: %v", tt.to, err)
				}
			} else {
				if err == nil {
					t.Fatalf("TransitionTo(%q) returned nil error, want %q", tt.to, tt.wantErr)
				}

				if err.Error() != tt.wantErr {
					t.Errorf("err = %q, want %q", err.Error(), tt.wantErr)
				}

				if !errors.Is(err, fsm.ErrInvalidTransition) {
					t.Errorf("err = %v, want it to wrap ErrInvalidTransition", err)
				}

				assertNoEventVocabulary(t, err)
			}

			if got := m.Current(); got != tt.want {
				t.Errorf("Current() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A failed resolve on this surface still names both ends. The engine cannot, having found no edge, but the caller
// passed the target and simplify restores it.
func TestMachineTransitionToNamesBothEnds(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustNew(statusGraph(), stateDraft)

	err := m.TransitionTo(ctx, statePaid)
	if err == nil {
		t.Fatal("TransitionTo returned nil error")
	}

	for _, want := range []string{"Draft", "Paid"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err.Error(), want)
		}
	}
}

func TestMachineCanTransitionTo(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustNew(statusGraph(), stateDraft)

	if err := m.CanTransitionTo(ctx, stateReview); err != nil {
		t.Errorf("CanTransitionTo(Review) = %v, want nil", err)
	}

	err := m.CanTransitionTo(ctx, statePaid)
	if err == nil {
		t.Error("CanTransitionTo(Paid) = nil, want an error")
	}

	assertNoEventVocabulary(t, err)

	if got := m.Current(); got != stateDraft {
		t.Errorf("CanTransitionTo moved the machine to %q", got)
	}
}

func TestMachineHooksReceiveChange(t *testing.T) {
	ctx := context.Background()

	t.Run("hooks receive a Change carrying both ends", func(t *testing.T) {
		m := fsm.MustNew(statusGraph(), stateDraft)

		var exit, enter fsm.Change[orderState]
		m.OnExit(stateDraft, func(_ context.Context, c fsm.Change[orderState]) error {
			exit = c

			return nil
		}).OnEnter(stateReview, func(_ context.Context, c fsm.Change[orderState]) error {
			enter = c

			return nil
		})

		if err := m.TransitionTo(ctx, stateReview); err != nil {
			t.Fatalf("TransitionTo returned error: %v", err)
		}

		if exit.From != stateDraft || exit.To != stateReview {
			t.Errorf("exit hook saw %+v, want Draft -> Review", exit)
		}

		if enter.From != stateDraft || enter.To != stateReview {
			t.Errorf("enter hook saw %+v, want Draft -> Review", enter)
		}
	})
}

func TestMachineHookBlocking(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")

	t.Run("a blocking exit hook aborts and the message names no event", func(t *testing.T) {
		m := fsm.MustNew(statusGraph(), stateDraft)
		m.OnExitBlocking(stateDraft, func(context.Context, fsm.Change[orderState]) error {
			return boom
		})

		err := m.TransitionTo(ctx, stateReview)
		if err == nil {
			t.Fatal("TransitionTo returned nil error, want the abort")
		}

		if !errors.Is(err, boom) {
			t.Errorf("err = %v, want it to wrap the hook's error", err)
		}

		assertNoEventVocabulary(t, err)

		if got := m.Current(); got != stateDraft {
			t.Errorf("Current() = %q, want %q", got, stateDraft)
		}
	})

	t.Run("a reporting exit hook does not stop the move", func(t *testing.T) {
		m := fsm.MustNew(statusGraph(), stateDraft)
		m.OnExit(stateDraft, func(context.Context, fsm.Change[orderState]) error {
			return boom
		})

		if err := m.TransitionTo(ctx, stateReview); err == nil {
			t.Fatal("TransitionTo returned nil error, want the reported error")
		}

		if got := m.Current(); got != stateReview {
			t.Errorf("Current() = %q, want %q", got, stateReview)
		}
	})

	t.Run("an enter hook error is reported after the move", func(t *testing.T) {
		m := fsm.MustNew(statusGraph(), stateDraft)
		m.OnEnter(stateReview, func(context.Context, fsm.Change[orderState]) error {
			return boom
		})

		err := m.TransitionTo(ctx, stateReview)
		if err == nil {
			t.Fatal("TransitionTo returned nil error, want the enter hook's error")
		}

		assertNoEventVocabulary(t, err)

		if got := m.Current(); got != stateReview {
			t.Errorf("Current() = %q, want %q", got, stateReview)
		}
	})
}

func TestMachineBothHooksFail(t *testing.T) {
	ctx := context.Background()

	t.Run("both hooks failing reports both, and neither names an event", func(t *testing.T) {
		exitErr := errors.New("metrics failed")
		enterErr := errors.New("notify failed")

		m := fsm.MustNew(statusGraph(), stateDraft)
		m.OnExit(stateDraft, func(context.Context, fsm.Change[orderState]) error {
			return exitErr
		}).OnEnter(stateReview, func(context.Context, fsm.Change[orderState]) error {
			return enterErr
		})

		err := m.TransitionTo(ctx, stateReview)
		if !errors.Is(err, exitErr) || !errors.Is(err, enterErr) {
			t.Errorf("err = %v, want both hook errors reachable", err)
		}

		assertNoEventVocabulary(t, err)
	})
}

// On this surface a guard keys on the edge from -> to, the same key the engine uses.
func TestMachineGuard(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustNew(statusGraph(), stateDraft)

	m.Guard(stateDraft, stateReview, func(context.Context, fsm.Change[orderState]) error {
		return errors.New("order has no items")
	})

	err := m.CanTransitionTo(ctx, stateReview)
	if err == nil {
		t.Fatal("CanTransitionTo(Review) = nil, want the guard's refusal")
	}

	assertNoEventVocabulary(t, err)

	if err := m.CanTransitionTo(ctx, stateCanceled); err != nil {
		t.Errorf("CanTransitionTo(Canceled) = %v, want nil; an unrelated edge must be unguarded", err)
	}
}

func TestMachineForceStateAndPredicates(t *testing.T) {
	m := fsm.MustNew(statusGraph(), stateShipped)

	if !m.Is(stateShipped) {
		t.Error("Is(Shipped) = false, want true")
	}

	if got := m.String(); got != "Shipped" {
		t.Errorf("String() = %q, want %q", got, "Shipped")
	}

	if err := m.ForceState(stateDraft); err != nil {
		t.Fatalf("ForceState returned error: %v", err)
	}

	if got := m.Current(); got != stateDraft {
		t.Errorf("Current() = %q, want %q", got, stateDraft)
	}

	err := m.ForceState(stateUnknown)
	if !errors.Is(err, fsm.ErrUnknownState) {
		t.Errorf("err = %v, want it to wrap ErrUnknownState", err)
	}

	assertNoEventVocabulary(t, err)
}

// Reentrancy is refused on this surface as well.
func TestMachineReentrancy(t *testing.T) {
	ctx := context.Background()
	m := fsm.MustNew(statusGraph(), stateDraft)

	var nested error
	m.OnEnter(stateReview, func(c context.Context, _ fsm.Change[orderState]) error {
		nested = m.TransitionTo(c, statePaid)

		return nil
	})

	if err := m.TransitionTo(ctx, stateReview); err != nil {
		t.Fatalf("the outer TransitionTo returned error: %v", err)
	}

	if !errors.Is(nested, fsm.ErrReentrant) {
		t.Errorf("nested TransitionTo returned %v, want ErrReentrant", nested)
	}

	if got := m.Current(); got != stateReview {
		t.Errorf("Current() = %q, want %q", got, stateReview)
	}
}

func ExampleMachine_TransitionTo() {
	ctx := context.Background()
	m := fsm.MustNew(statusGraph(), stateDraft)

	if err := m.TransitionTo(ctx, stateReview); err != nil {
		fmt.Println(err)
	}

	fmt.Println(m.Current())

	// The graph declares no edge from Review back to Draft.
	if err := m.TransitionTo(ctx, stateDraft); err != nil {
		fmt.Println(err)
	}

	fmt.Println(m.Current())

	// Output:
	// Review
	// fsm: cannot transition Review -> Draft: invalid transition
	// Review
}

func ExampleMachine_OnEnter() {
	ctx := context.Background()
	m := fsm.MustNew(statusGraph(), stateDraft)

	m.OnEnter(stateReview, func(_ context.Context, c fsm.Change[orderState]) error {
		fmt.Printf("moved %s -> %s\n", c.From, c.To)

		return nil
	})

	if err := m.TransitionTo(ctx, stateReview); err != nil {
		fmt.Println(err)
	}

	// Output:
	// moved Draft -> Review
}

// A nil hook is ignored rather than stored and called, so it cannot panic during a transition.
func TestNilHooksAreIgnored(t *testing.T) {
	ctx := context.Background()

	t.Run("on the simple surface", func(t *testing.T) {
		m := fsm.MustNew(statusGraph(), stateDraft)

		m.Guard(stateDraft, stateReview, nil).
			OnExit(stateDraft, nil).
			OnEnter(stateReview, nil)

		if err := m.TransitionTo(ctx, stateReview); err != nil {
			t.Fatalf("TransitionTo returned error: %v", err)
		}

		if got := m.Current(); got != stateReview {
			t.Errorf("Current() = %q, want %q", got, stateReview)
		}
	})

	t.Run("on the labeled surface", func(t *testing.T) {
		m := fsm.MustEventMachine(orderGraph(), stateDraft)

		m.Guard(stateDraft, eventSubmit, nil).
			OnExit(stateDraft, nil).
			OnExitBlocking(stateDraft, nil).
			OnEnter(stateReview, nil)

		if err := m.Fire(ctx, eventSubmit); err != nil {
			t.Fatalf("Fire returned error: %v", err)
		}

		if got := m.Current(); got != stateReview {
			t.Errorf("Current() = %q, want %q", got, stateReview)
		}
	})
}
