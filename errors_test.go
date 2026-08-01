package fsm_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/behzadsh/fsm"
)

func TestPhaseString(t *testing.T) {
	tests := []struct {
		name  string
		phase fsm.Phase
		want  string
	}{
		{"resolve", fsm.PhaseResolve, "resolve"},
		{"guard", fsm.PhaseGuard, "guard"},
		{"exit", fsm.PhaseExit, "exit"},
		{"enter", fsm.PhaseEnter, "enter"},
		{"a value outside the enum still renders", fsm.Phase(99), "Phase(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.phase.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// PhaseResolve must be the zero value, so a TransitionError built without an explicit phase reports the earliest
// stage rather than an arbitrary one.
func TestPhaseZeroValue(t *testing.T) {
	var p fsm.Phase

	if p != fsm.PhaseResolve {
		t.Errorf("zero Phase = %v, want PhaseResolve", p)
	}
}

func TestTransitionErrorError(t *testing.T) {
	tests := []struct {
		name string
		err  *fsm.TransitionError[orderState, orderEvent]
		want string
	}{
		{
			// Resolve failed, so no target was ever found and To is the zero value. The
			// message must not pretend to know a destination.
			"resolve, with no target to name",
			&fsm.TransitionError[orderState, orderEvent]{
				From:  stateDraft,
				Event: eventUnknown,
				Phase: fsm.PhaseResolve,
				Err:   fsm.ErrInvalidTransition,
			},
			"fsm: cannot fire unknown from Draft: invalid transition",
		},
		{
			"guard, naming both ends",
			&fsm.TransitionError[orderState, orderEvent]{
				From:  stateDraft,
				To:    stateReview,
				Event: eventSubmit,
				Phase: fsm.PhaseGuard,
				Err:   errors.New("not authorized"),
			},
			"fsm: guard Draft -> Review (event submit): not authorized",
		},
		{
			"exit",
			&fsm.TransitionError[orderState, orderEvent]{
				From:  statePaid,
				To:    stateShipped,
				Event: eventShip,
				Phase: fsm.PhaseExit,
				Err:   errors.New("hold not released"),
			},
			"fsm: exit Paid -> Shipped (event ship): hold not released",
		},
		{
			"enter",
			&fsm.TransitionError[orderState, orderEvent]{
				From:  statePaid,
				To:    stateShipped,
				Event: eventShip,
				Phase: fsm.PhaseEnter,
				Err:   errors.New("notify failed"),
			},
			"fsm: enter Paid -> Shipped (event ship): notify failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTransitionErrorUnwrap(t *testing.T) {
	t.Run("errors.Is reaches the wrapped sentinel", func(t *testing.T) {
		err := error(&fsm.TransitionError[orderState, orderEvent]{
			From:  stateDraft,
			Event: eventUnknown,
			Phase: fsm.PhaseResolve,
			Err:   fsm.ErrInvalidTransition,
		})

		if !errors.Is(err, fsm.ErrInvalidTransition) {
			t.Error("errors.Is(err, ErrInvalidTransition) = false, want true")
		}

		if errors.Is(err, fsm.ErrUnknownState) {
			t.Error("errors.Is(err, ErrUnknownState) = true, want false")
		}
	})

	t.Run("errors.As extracts the structured error", func(t *testing.T) {
		inner := errors.New("not authorized")
		err := fmt.Errorf("handling request: %w", &fsm.TransitionError[orderState, orderEvent]{
			From:  stateDraft,
			To:    stateReview,
			Event: eventSubmit,
			Phase: fsm.PhaseGuard,
			Err:   inner,
		})

		var te *fsm.TransitionError[orderState, orderEvent]
		if !errors.As(err, &te) {
			t.Fatal("errors.As did not extract *TransitionError")
		}

		if te.Phase != fsm.PhaseGuard {
			t.Errorf("Phase = %v, want PhaseGuard", te.Phase)
		}

		if te.From != stateDraft || te.To != stateReview || te.Event != eventSubmit {
			t.Errorf("got %s -> %s (event %s), want Draft -> Review (event submit)", te.From, te.To, te.Event)
		}

		if !errors.Is(err, inner) {
			t.Error("errors.Is did not reach the innermost error through the wrap chain")
		}
	})
}

// Moved draws the commit boundary that decides whether retrying is safe. It reports Committed rather than deriving the
// answer from Phase, because PhaseExit is ambiguous: a blocking exit hook aborts before the commit, while a reporting
// one lets the transition through and only carries its error alongside.
func TestTransitionErrorMoved(t *testing.T) {
	tests := []struct {
		name      string
		phase     fsm.Phase
		committed bool
	}{
		{"resolve found no edge, so nothing moved", fsm.PhaseResolve, false},
		{"a guard refused before anything ran", fsm.PhaseGuard, false},
		{"a blocking exit hook aborted before the commit", fsm.PhaseExit, false},
		{"a reporting exit hook failed but the move went through", fsm.PhaseExit, true},
		{"an enter hook failed after the commit", fsm.PhaseEnter, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &fsm.TransitionError[orderState, orderEvent]{
				From:      statePaid,
				To:        stateShipped,
				Event:     eventShip,
				Phase:     tt.phase,
				Committed: tt.committed,
				Err:       errors.New("boom"),
			}

			if got := err.Moved(); got != tt.committed {
				t.Errorf("Moved() with %v = %v, want %v", tt.phase, got, tt.committed)
			}
		})
	}
}

// The two PhaseExit rows above are the whole reason Moved reads a field: the phase alone cannot separate them.
func TestTransitionErrorMovedPhaseExitIsAmbiguous(t *testing.T) {
	base := func(committed bool) *fsm.TransitionError[orderState, orderEvent] {
		return &fsm.TransitionError[orderState, orderEvent]{
			From:      statePaid,
			To:        stateShipped,
			Event:     eventShip,
			Phase:     fsm.PhaseExit,
			Committed: committed,
			Err:       errors.New("boom"),
		}
	}

	if base(false).Moved() {
		t.Error("a blocking exit abort reports Moved() = true, want false")
	}

	if !base(true).Moved() {
		t.Error("a reporting exit failure reports Moved() = false, want true")
	}
}

// The three sentinels must be distinct, or a caller switching on them would match the wrong one.
func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := map[string]error{
		"ErrInvalidTransition": fsm.ErrInvalidTransition,
		"ErrReentrant":         fsm.ErrReentrant,
		"ErrUnknownState":      fsm.ErrUnknownState,
	}

	for nameA, a := range sentinels {
		for nameB, b := range sentinels {
			if nameA == nameB {
				continue
			}

			if errors.Is(a, b) {
				t.Errorf("errors.Is(%s, %s) = true, want false", nameA, nameB)
			}
		}
	}
}

func ExamplePhase() {
	// Phase answers the only question a caller has after a failure: did anything happen?
	fmt.Println(fsm.PhaseResolve, fsm.PhaseGuard, fsm.PhaseExit, fsm.PhaseEnter)

	// Output:
	// resolve guard exit enter
}

func ExampleTransitionError() {
	err := error(&fsm.TransitionError[orderState, orderEvent]{
		From:  statePaid,
		To:    stateShipped,
		Event: eventShip,
		Phase: fsm.PhaseEnter,
		Err:   errors.New("notify failed"),
	})

	fmt.Println(err)

	// PhaseEnter means the machine did move; only the follow-up failed, so the caller should log rather than retry.
	var te *fsm.TransitionError[orderState, orderEvent]
	if errors.As(err, &te) {
		fmt.Println(te.Phase == fsm.PhaseEnter)
	}

	// Output:
	// fsm: enter Paid -> Shipped (event ship): notify failed
	// true
}
