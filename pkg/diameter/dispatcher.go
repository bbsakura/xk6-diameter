package diameter

import (
	"sync"

	"github.com/fiorix/go-diameter/v4/diam"
)

// serveBufSize is the per-Serve subscription channel capacity. Drops
// happen non-blockingly when a slow consumer lets its buffer fill.
const serveBufSize = 256

// dispatcher routes incoming Diameter messages that were NOT matched
// by the HBH-ID correlationTbl to caller-registered subscribers.
//
// Precedence per message:
//  1. correlationTbl (Send*/CheckSend* pending) — handled upstream.
//  2. recvs (Receive one-shots) — FIFO, first match wins and is removed.
//  3. serves (Serve streams) — fan out to every matching subscription.
type dispatcher struct {
	mu     sync.Mutex
	recvs  []*recvSub
	serves []*serveSub
}

type recvSub struct {
	m  Matcher
	ch chan *diam.Message // buffered=1
}

type serveSub struct {
	m      Matcher
	ch     chan *diam.Message // buffered=serveBufSize
	closed bool               // guarded by dispatcher.mu
}

func newDispatcher() *dispatcher {
	return &dispatcher{}
}

// registerRecv adds a one-shot receiver. Callers select on the returned
// channel, then call unregisterRecv on cancellation. Once dispatch has
// delivered to this receiver, the entry is auto-removed.
func (d *dispatcher) registerRecv(m Matcher) chan *diam.Message {
	sub := &recvSub{m: m, ch: make(chan *diam.Message, 1)}
	d.mu.Lock()
	d.recvs = append(d.recvs, sub)
	d.mu.Unlock()
	return sub.ch
}

// unregisterRecv removes a pending receiver by its channel identity.
// Safe to call after delivery (no-op).
func (d *dispatcher) unregisterRecv(ch chan *diam.Message) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, r := range d.recvs {
		if r.ch == ch {
			d.recvs = append(d.recvs[:i], d.recvs[i+1:]...)
			return
		}
	}
}

// registerServe adds a streaming subscription. Callers read from
// sub.ch and call unregisterServe on close.
func (d *dispatcher) registerServe(m Matcher) *serveSub {
	sub := &serveSub{m: m, ch: make(chan *diam.Message, serveBufSize)}
	d.mu.Lock()
	d.serves = append(d.serves, sub)
	d.mu.Unlock()
	return sub
}

// unregisterServe removes a Serve subscription and marks it closed so
// dispatch stops feeding it. Idempotent.
func (d *dispatcher) unregisterServe(target *serveSub) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, s := range d.serves {
		if s == target {
			s.closed = true
			d.serves = append(d.serves[:i], d.serves[i+1:]...)
			return
		}
	}
}

// dispatch routes msg per precedence rules. Returns whether any
// subscriber (Receive or Serve) accepted it.
func (d *dispatcher) dispatch(msg *diam.Message) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Receive wins: first match consumes and is removed.
	for i, r := range d.recvs {
		if r.m.match(msg) {
			d.recvs = append(d.recvs[:i], d.recvs[i+1:]...)
			select {
			case r.ch <- msg:
			default:
			}
			return true
		}
	}
	// Serve fan-out.
	delivered := false
	for _, s := range d.serves {
		if s.closed || !s.m.match(msg) {
			continue
		}
		select {
		case s.ch <- msg:
			delivered = true
		default:
			// buffer full; drop
		}
	}
	return delivered
}
