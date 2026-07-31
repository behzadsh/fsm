package fsm_test

import (
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
)

const (
	eventSubmit   orderEvent = "submit"
	eventResubmit orderEvent = "resubmit"
	eventCancel   orderEvent = "cancel"
	eventPay      orderEvent = "pay"
	eventShip     orderEvent = "ship"
)

// orderGraph is the graph most tests work against:
//
//	Draft   ----submit----> Review      Draft  ---cancel---> Canceled
//	Draft   ---resubmit---> Review      Review ---cancel---> Canceled
//	Review  ------pay-----> Paid        Paid   ---cancel---> Refunded
//	Paid    -----ship-----> Shipped
//
// It deliberately contains three properties the design has to handle: two events reaching the same target (submit and
// resubmit both land on Review), one event fanning out to different targets from different sources (cancel), and
// terminal states with no outgoing edges (Shipped, Canceled, Refunded).
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
