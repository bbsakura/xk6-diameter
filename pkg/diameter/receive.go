package diameter

import (
	"sync/atomic"

	"github.com/grafana/sobek"
	"github.com/pkg/errors"
	"go.k6.io/k6/js/promises"
)

// Receive registers a one-shot subscriber against the message
// dispatcher and returns a Promise that resolves with the first
// matching message. Zero-value matcher fields (missing keys in JS) are
// wildcards. Rejects on iteration end.
func (h *ClientHdr) Receive(rawMatcher map[string]interface{}) *sobek.Promise {
	matcher := MapToMatcher(rawMatcher)
	ch := h.Client.disp.registerRecv(matcher)
	p, resolve, reject := promises.New(h.vu)
	go func() {
		select {
		case msg := <-ch:
			resolve(msg)
		case <-h.vu.Context().Done():
			h.Client.disp.unregisterRecv(ch)
			reject(errors.New("iteration ended"))
		}
	}()
	return p
}

// Subscribe registers a streaming subscriber. The returned Subscription
// exposes Recv() as a Promise that resolves with each next matching
// message; call Close() to unregister. Zero-value matcher = receive all.
//
//	const sub = conn.subscribe({ cmd_code: 316 });
//	try { for(;;) { const msg = await sub.recv(); ... } }
//	finally { sub.close(); }
func (h *ClientHdr) Subscribe(rawMatcher map[string]interface{}) *Subscription {
	return &Subscription{
		hdr:  h,
		sub:  h.Client.disp.registerServe(MapToMatcher(rawMatcher)),
		done: make(chan struct{}),
	}
}

// Serve registers a streaming subscriber whose messages are pushed to
// the JS callback cb via the k6 event loop. Returns a ServeHandle whose
// Close() unregisters and stops the callback loop. Zero-value matcher
// = receive all.
//
//	const sub = conn.serve({ app_id: 16777251 }, (msg) => { ... });
//	// later
//	sub.close();
func (h *ClientHdr) Serve(rawMatcher map[string]interface{}, cb sobek.Value) *ServeHandle {
	rt := h.vu.Runtime()
	fn, isFn := sobek.AssertFunction(cb)
	if !isFn {
		panic(rt.NewGoError(errors.New("Serve: cb must be a function")))
	}
	sh := &ServeHandle{
		hdr:  h,
		sub:  h.Client.disp.registerServe(MapToMatcher(rawMatcher)),
		done: make(chan struct{}),
	}
	go sh.run(fn)
	return sh
}

// Subscription is a JS-facing handle for a Subscribe stream (chan-like).
type Subscription struct {
	hdr    *ClientHdr
	sub    *serveSub
	done   chan struct{}
	closed atomic.Bool
}

// Recv returns a Promise that resolves with the next message this
// subscription accepts, or rejects when Close() is called or the
// iteration ends. Safe to call repeatedly in a JS loop.
func (s *Subscription) Recv() *sobek.Promise {
	p, resolve, reject := promises.New(s.hdr.vu)
	if s.closed.Load() {
		reject(errors.New("subscription closed"))
		return p
	}
	go func() {
		select {
		case <-s.done:
			reject(errors.New("subscription closed"))
		case msg := <-s.sub.ch:
			resolve(msg)
		case <-s.hdr.vu.Context().Done():
			reject(errors.New("iteration ended"))
		}
	}()
	return p
}

// Close unregisters the subscription and cancels any pending Recv.
// Idempotent.
func (s *Subscription) Close() {
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	close(s.done)
	s.hdr.Client.disp.unregisterServe(s.sub)
}

// ServeHandle is a JS-facing handle for a Serve stream (callback).
type ServeHandle struct {
	hdr    *ClientHdr
	sub    *serveSub
	done   chan struct{}
	closed atomic.Bool
}

// Close unregisters the subscription and stops the callback loop.
// Idempotent.
func (s *ServeHandle) Close() {
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	close(s.done)
	s.hdr.Client.disp.unregisterServe(s.sub)
}

// run drains sub.ch and dispatches each message to the JS callback via
// the k6 event loop. Returns when Close() is called.
func (s *ServeHandle) run(cb sobek.Callable) {
	vu := s.hdr.vu
	rt := vu.Runtime()
	for {
		select {
		case <-s.done:
			return
		case msg := <-s.sub.ch:
			enqueue := vu.RegisterCallback()
			m := msg
			enqueue(func() error {
				_, err := cb(sobek.Undefined(), rt.ToValue(m))
				return err
			})
		}
	}
}
