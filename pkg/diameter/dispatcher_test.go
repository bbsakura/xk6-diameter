package diameter

import (
	"testing"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

func newTestMsg(cmd uint32, request bool) *diam.Message {
	var flags uint8
	if request {
		flags = diam.RequestFlag
	}
	return diam.NewMessage(cmd, flags, diam.TGPP_S6A_APP_ID, 0, 0, dict.Default)
}

func TestDispatcher_Recv_MatchAndAutoRemove(t *testing.T) {
	d := newDispatcher()
	ch := d.registerRecv(Matcher{CmdCode: ptrU32(diam.AuthenticationInformation)})

	if !d.dispatch(newTestMsg(diam.AuthenticationInformation, true)) {
		t.Fatalf("first dispatch should hit the recv")
	}
	select {
	case <-ch:
	default:
		t.Fatalf("recv channel should have the message")
	}
	// Second dispatch: the recv was auto-removed; nothing to deliver.
	if d.dispatch(newTestMsg(diam.AuthenticationInformation, true)) {
		t.Fatalf("second dispatch should have no subscriber")
	}
}

func TestDispatcher_Recv_FIFO_OldestWins(t *testing.T) {
	d := newDispatcher()
	// Both match; oldest (ch1) must win.
	ch1 := d.registerRecv(Matcher{})
	ch2 := d.registerRecv(Matcher{})

	d.dispatch(newTestMsg(1, true))
	select {
	case <-ch1:
	default:
		t.Fatalf("oldest recv did not receive")
	}
	select {
	case <-ch2:
		t.Fatalf("later recv should not have received")
	default:
	}
}

func TestDispatcher_ReceivePrecedesServe(t *testing.T) {
	d := newDispatcher()
	serveSub := d.registerServe(Matcher{})
	recvCh := d.registerRecv(Matcher{})

	d.dispatch(newTestMsg(1, true))
	select {
	case <-recvCh:
	default:
		t.Fatalf("Receive should win over Serve")
	}
	select {
	case <-serveSub.ch:
		t.Fatalf("Serve must not receive when Receive matched")
	default:
	}
}

func TestDispatcher_Serve_FanOut(t *testing.T) {
	d := newDispatcher()
	s1 := d.registerServe(Matcher{})
	s2 := d.registerServe(Matcher{})
	s3 := d.registerServe(Matcher{CmdCode: ptrU32(999)}) // non-match

	d.dispatch(newTestMsg(1, true))
	for i, s := range []*serveSub{s1, s2} {
		select {
		case <-s.ch:
		default:
			t.Fatalf("serve %d should have received", i)
		}
	}
	select {
	case <-s3.ch:
		t.Fatalf("non-matching serve should not receive")
	default:
	}
}

func TestDispatcher_Serve_BufferFullDrop(t *testing.T) {
	d := newDispatcher()
	sub := d.registerServe(Matcher{})
	// Fill the buffer (256) + 1 extra to trigger drop.
	for i := 0; i < serveBufSize+1; i++ {
		d.dispatch(newTestMsg(uint32(i), true))
	}
	if len(sub.ch) != serveBufSize {
		t.Fatalf("buffer len = %d; want %d", len(sub.ch), serveBufSize)
	}
	// Drain to ensure no goroutine got stuck.
	drained := 0
	for {
		select {
		case <-sub.ch:
			drained++
		default:
			if drained != serveBufSize {
				t.Fatalf("drained %d; want %d", drained, serveBufSize)
			}
			return
		}
	}
}

func TestDispatcher_UnregisterServe(t *testing.T) {
	d := newDispatcher()
	sub := d.registerServe(Matcher{})
	d.unregisterServe(sub)
	if d.dispatch(newTestMsg(1, true)) {
		t.Fatalf("dispatch after unregister should have no subscriber")
	}
	// Idempotent second call.
	d.unregisterServe(sub)
}

func TestDispatcher_UnregisterRecv(t *testing.T) {
	d := newDispatcher()
	ch := d.registerRecv(Matcher{})
	d.unregisterRecv(ch)
	if d.dispatch(newTestMsg(1, true)) {
		t.Fatalf("dispatch after unregister should have no subscriber")
	}
	// Idempotent.
	d.unregisterRecv(ch)
}
