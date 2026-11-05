package domain

import (
	"errors"
	"testing"
	"time"
)

var allStatuses = []Status{
	StatusPendingPayment, StatusPaid, StatusFulfilling, StatusShipped,
	StatusDelivered, StatusCancelled, StatusRefundPending, StatusRefunded,
}

// The legal moves, written out independently of the implementation.
var legalMoves = [][2]Status{
	{StatusPendingPayment, StatusPaid},
	{StatusPendingPayment, StatusCancelled},
	{StatusPaid, StatusFulfilling},
	{StatusPaid, StatusRefundPending},
	{StatusFulfilling, StatusShipped},
	{StatusFulfilling, StatusRefundPending},
	{StatusShipped, StatusDelivered},
	{StatusDelivered, StatusRefundPending},
	{StatusRefundPending, StatusRefunded},
}

var testTime = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

func newOrder(t *testing.T, s Status) *Order {
	t.Helper()
	o, err := RehydrateOrder(mustID(t, sampleID), mustID(t, otherID), s, nil)
	if err != nil {
		t.Fatalf("RehydrateOrder(%s): %v", s, err)
	}
	return o
}

func TestTransitionMatrix(t *testing.T) {
	legal := map[[2]Status]bool{}
	for _, m := range legalMoves {
		legal[m] = true
	}
	for _, from := range allStatuses {
		for _, to := range allStatuses {
			o := newOrder(t, from)
			err := o.Transition(to, "test", "", testTime)

			if legal[[2]Status{from, to}] {
				if err != nil {
					t.Errorf("%s -> %s: want success, got %v", from, to, err)
				} else if o.Status() != to || len(o.History()) != 1 {
					t.Errorf("%s -> %s: status=%s history=%d", from, to, o.Status(), len(o.History()))
				}
				continue
			}
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("%s -> %s: want ErrInvalidTransition, got %v", from, to, err)
			}
			if o.Status() != from || len(o.History()) != 0 {
				t.Errorf("%s -> %s: order changed on error", from, to)
			}
		}
	}
}

func TestTransition_UnknownStatuses(t *testing.T) {
	for _, bad := range []Status{"", "BOGUS", "paid"} {
		if err := newOrder(t, StatusPendingPayment).Transition(bad, "test", "", testTime); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("to %q: want ErrInvalidTransition, got %v", bad, err)
		}
	}
	var zero Order
	if err := zero.Transition(StatusPaid, "test", "", testTime); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("zero Order: want ErrInvalidTransition, got %v", err)
	}
}

func TestTransition_RequiresActorAndTime(t *testing.T) {
	o := newOrder(t, StatusPendingPayment)
	if err := o.Transition(StatusPaid, "", "", testTime); !errors.Is(err, ErrEmptyActor) {
		t.Errorf("want ErrEmptyActor, got %v", err)
	}
	if err := o.Transition(StatusPaid, "test", "", time.Time{}); !errors.Is(err, ErrZeroTime) {
		t.Errorf("want ErrZeroTime, got %v", err)
	}
	if o.Status() != StatusPendingPayment || len(o.History()) != 0 {
		t.Error("order changed on error")
	}
}

func TestTransition_RecordsHistory(t *testing.T) {
	o := newOrder(t, StatusPendingPayment)
	if err := o.Transition(StatusPaid, "system:webhook", "payment confirmed", testTime); err != nil {
		t.Fatal(err)
	}
	want := StatusChange{From: StatusPendingPayment, To: StatusPaid, Actor: "system:webhook", Reason: "payment confirmed", At: testTime}
	if h := o.History(); len(h) != 1 || h[0] != want {
		t.Fatalf("history = %+v, want [%+v]", h, want)
	}
}

func TestHistoryIsCopied(t *testing.T) {
	in := []StatusChange{{From: StatusPendingPayment, To: StatusPaid, Actor: "x", At: testTime}}
	o, err := RehydrateOrder(mustID(t, sampleID), mustID(t, otherID), StatusPaid, in)
	if err != nil {
		t.Fatal(err)
	}
	in[0].Actor = "changed"
	o.History()[0].Actor = "changed"
	if o.History()[0].Actor != "x" {
		t.Fatal("order history was modified from outside")
	}
}

func TestRehydrateOrder_RejectsInvalidStatus(t *testing.T) {
	if _, err := RehydrateOrder(ID{}, ID{}, "BOGUS", nil); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("want ErrInvalidStatus, got %v", err)
	}
}

func TestOrder_Accessors(t *testing.T) {
	o := newOrder(t, StatusShipped)
	if o.ID() != mustID(t, sampleID) || o.UserID() != mustID(t, otherID) || o.Status() != StatusShipped {
		t.Fatalf("accessors wrong: %s %s %s", o.ID(), o.UserID(), o.Status())
	}
}