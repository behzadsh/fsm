package fsm_test

import (
	"strings"
	"testing"

	"github.com/behzadsh/fsm"
)

// orderState and orderEvent are the vocabularies every test and example in this package works against. They are
// distinct named string types, which is how callers are expected to declare their own.
type (
	orderState string
	orderEvent string
)

// The order lifecycle used throughout the tests.
const (
	stateDraft    orderState = "Draft"
	stateReview   orderState = "Review"
	statePaid     orderState = "Paid"
	stateShipped  orderState = "Shipped"
	stateCanceled orderState = "Canceled"
	stateRefunded orderState = "Refunded"

	// Deliberately absent from every graph in this package.
	stateUnknown orderState = "Unknown"
)

const (
	eventSubmit   orderEvent = "submit"
	eventResubmit orderEvent = "resubmit"
	eventCancel   orderEvent = "cancel"
	eventPay      orderEvent = "pay"
	eventShip     orderEvent = "ship"

	// Deliberately absent from every graph in this package.
	eventUnknown orderEvent = "unknown"
)

// orderGraph is the graph most tests work against:
//
//	Draft   ----submit----> Review      Draft  ---cancel---> Canceled
//	Draft   ---resubmit---> Review      Review ---cancel---> Canceled
//	Review  ------pay-----> Paid        Paid   ---cancel---> Refunded
//	Paid    -----ship-----> Shipped
//
// It covers three shapes the package has to handle: two events reaching the same target (submit and resubmit both
// land on Review), one event leading to different targets from different sources (cancel), and terminal states with no
// outgoing edges (Shipped, Canceled, Refunded).
//
// Each call returns a freshly built graph, so a test may not affect any other.
func orderGraph() fsm.EventGraph[orderState, orderEvent] {
	return fsm.NewEventGraph[orderState, orderEvent]().
		On(stateDraft, eventSubmit, stateReview).
		On(stateDraft, eventResubmit, stateReview).
		On(stateDraft, eventCancel, stateCanceled).
		On(stateReview, eventPay, statePaid).
		On(stateReview, eventCancel, stateCanceled).
		On(statePaid, eventShip, stateShipped).
		On(statePaid, eventCancel, stateRefunded).
		MustBuild()
}

// statusGraph is the same lifecycle expressed without events, for the simple surface:
//
//	Draft   -> Review, Canceled
//	Review  -> Paid, Canceled
//	Paid    -> Shipped, Refunded
//	Shipped, Canceled, Refunded: terminal
//
// Each call returns a freshly built graph, so a test may not affect any other.
func statusGraph() fsm.Graph[orderState] {
	return fsm.NewGraph[orderState]().
		To(stateDraft, stateReview, stateCanceled).
		To(stateReview, statePaid, stateCanceled).
		To(statePaid, stateShipped, stateRefunded).
		MustBuild()
}

// assertNoEventVocabulary fails when an error from the simple surface mentions events. That surface runs on the
// labeled engine, and this checks the binding stays hidden.
func assertNoEventVocabulary(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		return
	}

	for _, leak := range []string{"event", "fire", "Fire"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q contains %q; the simple surface must not mention events", err.Error(), leak)
		}
	}
}
