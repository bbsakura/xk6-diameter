package diameter

import (
	"sync"
	"testing"
	"time"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// newMsg builds an Answer with the given HBH-ID. Answers are the only
// message class the correlation table routes; see
// TestCorrelationTbl_RejectsRequest for the Request path.
func newMsg(hbhID uint32) *diam.Message {
	m := diam.NewMessage(diam.AuthenticationInformation, 0, diam.TGPP_S6A_APP_ID, 0, 0, dict.Default)
	m.Header.HopByHopID = hbhID
	return m
}

func newRequestMsg(hbhID uint32) *diam.Message {
	m := diam.NewRequest(diam.AuthenticationInformation, diam.TGPP_S6A_APP_ID, dict.Default)
	m.Header.HopByHopID = hbhID
	return m
}

func TestCorrelationTbl_RegisterDeliverUnregister(t *testing.T) {
	tbl := newCorrelationTbl()
	ch := tbl.register(42)

	if !tbl.deliver(newMsg(42)) {
		t.Fatalf("deliver(42) returned false; want true")
	}
	select {
	case got := <-ch:
		if got.Header.HopByHopID != 42 {
			t.Fatalf("received hbh=%d; want 42", got.Header.HopByHopID)
		}
	default:
		t.Fatalf("channel empty after deliver")
	}

	tbl.unregister(42)
	if tbl.deliver(newMsg(42)) {
		t.Fatalf("deliver after unregister returned true; want false")
	}
}

func TestCorrelationTbl_OrphanDrop(t *testing.T) {
	tbl := newCorrelationTbl()
	if tbl.deliver(newMsg(99)) {
		t.Fatalf("deliver of unknown hbh returned true; want false")
	}
}

// TestCorrelationTbl_RejectsRequest guards against Requests being
// mis-routed as Answers when their HBH-ID coincides with a pending
// client Send. HBH-IDs are per-sender, so this collision is legal.
func TestCorrelationTbl_RejectsRequest(t *testing.T) {
	tbl := newCorrelationTbl()
	ch := tbl.register(11)

	if tbl.deliver(newRequestMsg(11)) {
		t.Fatalf("Request with matching HBH-ID must not correlate")
	}
	select {
	case msg := <-ch:
		t.Fatalf("Answer channel unexpectedly received: %+v", msg.Header)
	default:
	}
}

func TestCorrelationTbl_DeliverNonBlockingOnFullChannel(t *testing.T) {
	tbl := newCorrelationTbl()
	tbl.register(7)

	// First deliver fills the buffered=1 channel.
	if !tbl.deliver(newMsg(7)) {
		t.Fatalf("first deliver returned false")
	}
	// Second deliver must not block; it should drop and return false.
	done := make(chan bool, 1)
	go func() { done <- tbl.deliver(newMsg(7)) }()

	select {
	case ok := <-done:
		if ok {
			t.Fatalf("second deliver returned true on full channel; want false (dropped)")
		}
	case <-time.After(time.Second):
		t.Fatalf("second deliver blocked; expected non-blocking drop")
	}
}

func TestCorrelationTbl_ConcurrentRegisterDeliver(t *testing.T) {
	tbl := newCorrelationTbl()
	const n = 200

	var wg sync.WaitGroup
	channels := make([]chan *diam.Message, n)
	for i := 0; i < n; i++ {
		channels[i] = tbl.register(uint32(i))
	}

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(id uint32) {
			defer wg.Done()
			if !tbl.deliver(newMsg(id)) {
				t.Errorf("deliver(%d) returned false", id)
			}
		}(uint32(i))
	}
	wg.Wait()

	for i, ch := range channels {
		select {
		case got := <-ch:
			if got.Header.HopByHopID != uint32(i) {
				t.Errorf("channel %d got hbh=%d; want %d", i, got.Header.HopByHopID, i)
			}
		default:
			t.Errorf("channel %d empty", i)
		}
	}
}
