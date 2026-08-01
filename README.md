# fsm

[![Go Reference](https://pkg.go.dev/badge/github.com/behzadsh/fsm.svg)](https://pkg.go.dev/github.com/behzadsh/fsm)
[![CI](https://github.com/behzadsh/fsm/actions/workflows/ci.yml/badge.svg)](https://github.com/behzadsh/fsm/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/behzadsh/fsm)](https://goreportcard.com/report/github.com/behzadsh/fsm)
[![Go Version](https://img.shields.io/github/go-mod/go-version/behzadsh/fsm)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A finite state machine for Go. A graph declares the states and the transitions allowed between them. A machine holds
one current state and rejects any transition the graph does not list.

States and events are your own named string types, so the compiler rejects an article's states where an order's are
expected. No dependencies outside the standard library.

## Install

```
go get github.com/behzadsh/fsm
```

Requires Go 1.21 or newer.

## Two surfaces

A graph belongs to one surface or the other. They share an engine but do not mix.

### Simple: name the destination

```go
package main

import (
	"context"
	"fmt"

	"github.com/behzadsh/fsm"
)

type OrderState string

const (
	StateDraft    OrderState = "Draft"
	StateReview   OrderState = "Review"
	StatePaid     OrderState = "Paid"
	StateShipped  OrderState = "Shipped"
	StateCanceled OrderState = "Canceled"
	StateRefunded OrderState = "Refunded"
)

var orderGraph = fsm.NewGraph[OrderState]().
	To(StateDraft, StateReview, StateCanceled).
	To(StateReview, StatePaid, StateCanceled).
	To(StatePaid, StateShipped).
	MustBuild()

func main() {
	ctx := context.Background()

	m := fsm.MustNew(orderGraph, StateDraft)

	if err := m.TransitionTo(ctx, StateReview); err != nil {
		fmt.Println(err)
	}

	fmt.Println(m.Current()) // Review

	if err := m.TransitionTo(ctx, StateShipped); err != nil {
		fmt.Println(err) // fsm: cannot transition Review -> Shipped: invalid transition
	}
}
```

### Labeled: name the action

An action may lead to different states depending on where the machine is. Naming only the destination cannot express
that, so the two cancels below need the labeled surface.

```go
type OrderEvent string

const (
	EventSubmit OrderEvent = "submit"
	EventPay    OrderEvent = "pay"
	EventShip   OrderEvent = "ship"
	EventCancel OrderEvent = "cancel"
	EventReject OrderEvent = "reject"
)

var orderGraph = fsm.NewEventGraph[OrderState, OrderEvent]().
	On(StateDraft, EventSubmit, StateReview).
	On(StateDraft, EventCancel, StateCanceled).
	On(StateReview, EventPay, StatePaid).
	On(StatePaid, EventCancel, StateRefunded).
	MustBuild()

m := fsm.MustEventMachine(orderGraph, StateDraft)

err := m.Fire(ctx, EventCancel)
```

The simple surface is the labeled one with each edge's event name bound to its target state. That binding does not
appear in its types, methods, errors, or hook arguments.

## Guards and hooks

A transition runs these stages in order:

| Stage | Side effects | Can block | Runs during `Can*` |
| --- | --- | --- | --- |
| resolve | no | yes, when no edge exists | yes |
| `Guard` | must not | yes | yes |
| `OnExit` / `OnExitBlocking` | yes | only when registered as blocking | no |
| the state changes | no | no | no |
| `OnEnter` | yes | no | no |

```go
m.Guard(StatePaid, EventShip, func(ctx context.Context, t fsm.Transition[OrderState, OrderEvent]) error {
	if order.Address == "" {
		return ErrNoAddress
	}
	return nil
}).OnExitBlocking(StatePaid, func(ctx context.Context, t fsm.Transition[OrderState, OrderEvent]) error {
	return releaseHold(ctx, order)
}).OnEnter(StateShipped, func(ctx context.Context, t fsm.Transition[OrderState, OrderEvent]) error {
	return notify(ctx, order)
})
```

Guards must be free of side effects, because `CanFire` and `CanTransitionTo` call them without moving the machine.

Work that should prevent a move when it fails belongs in `OnExitBlocking`. It runs before the state changes, so a
failure leaves nothing to undo.

A guard is registered per edge and a hook per state and phase. Registering again replaces the previous one.

## Errors

```go
var (
	ErrInvalidTransition = errors.New("invalid transition")
	ErrReentrant         = errors.New("reentrant call from hook")
	ErrUnknownState      = errors.New("unknown state")
)
```

The labeled surface returns `*TransitionError[S, E]` carrying `From`, `To`, `Event`, `Phase`, and the cause. `Moved`
reports whether the state changed before the failure, which determines whether the call can be retried.

```go
var te *fsm.TransitionError[OrderState, OrderEvent]
if errors.As(err, &te) {
	if te.Moved() {
		log.Warn("order moved, follow-up failed", "err", te.Err)
		return nil
	}
	return err
}
```

Read `Moved` rather than comparing `Phase`. `PhaseExit` covers both a blocking hook that aborted before the change and
a reporting hook that did not stop it.

The simple surface returns errors built with `fmt.Errorf`, so `errors.Is` matches the sentinels but `errors.As` and
`Moved` are unavailable. A structured error there would have to name the event type that surface hides.

## Semantics

- `CanFire` and `CanTransitionTo` check the edge and the guard. They report that a move is allowed, not that it will
  succeed: a blocking exit hook can still abort it.
- Machines hold no lock and are not safe for concurrent use. Callers synchronize.
- `ForceState` sets the state without consulting the graph, guards, or hooks. It is meant for operational repair.
- A hook cannot move its own machine. Nested calls return `ErrReentrant` and leave the outer transition alone.
- A built graph is sealed. Later use of its builder does not change what it allows.
- `New` and `NewEventMachine` reject an initial state the graph does not name.

## Going backwards

There is no `Rollback`. Four different needs hide behind that word:

| Need | Answer |
| --- | --- |
| A backward move that is domain logic | Declare the edge: `On(StateReview, EventReject, StateDraft)` |
| Undo after a side effect failed | Move the work into `OnExitBlocking`, so it never commits |
| Restore from storage | `New(graph, storedState)`, which validates it |
| Operational repair | `ForceState` |

A declared backward edge is auditable, can be guarded and hooked, and appears in `Mermaid()` like any other
transition.

## Seeing the shape

A builder chain does not show the machine at a glance the way a map literal does, so graphs render themselves.

```go
fmt.Println(orderGraph.Mermaid())
// stateDiagram-v2
//     Draft --> Canceled: cancel
//     Draft --> Review: submit
//     Review --> Paid: pay

fmt.Println(orderGraph)
// Draft ---cancel---> Canceled
// Draft ---submit---> Review
// Review ---pay---> Paid
```

Both outputs are sorted, so they are stable across runs and usable as golden files.

## Embedding

```go
type Order struct {
	*fsm.EventMachine[OrderState, OrderEvent]

	ID string
}

func NewOrder(id string) *Order {
	return &Order{EventMachine: fsm.MustEventMachine(orderGraph, StateDraft), ID: id}
}

func (o *Order) IsTerminal() bool {
	return o.Is(StateShipped) || o.Is(StateCanceled) || o.Is(StateRefunded)
}
```

To restore from storage, pass the stored state to `New` or `NewEventMachine`. A machine holds nothing else.

## Development

```sh
go test ./...                                                    # all tests, including Example output
go test -run TestEventMachineFire ./...                          # one test
go test -race -coverprofile=coverage.out -covermode=atomic ./... # what CI runs
go vet ./...
golangci-lint run                                                # config in .golangci.yml; test files are linted too
golangci-lint fmt ./...                                          # apply gci, gofmt, gofumpt, golines
```

CI runs every Go minor from the declared floor through the current release, and fails when coverage drops below 95%.

Comments wrap at 120 characters. Tests live in the external `fsm_test` package, one test file per source file,
table-driven with `t.Run` subtests, plus an `Example` per exported symbol whose `// Output:` block `go test` verifies.

## License

MIT. See [LICENSE](LICENSE).
