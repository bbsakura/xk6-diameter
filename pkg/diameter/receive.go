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
// wildcards; mistyped fields throw. Rejects on iteration end.
func (h *ClientHdr) Receive(rawMatcher map[string]interface{}) *sobek.Promise {
	matcher, err := MapToMatcher(rawMatcher)
	if err != nil {
		panic(h.vu.Runtime().NewGoError(err))
	}
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
//	const sub = conn.subscribe({ cmd_code: "CLR" });
//	try { for(;;) { const msg = await sub.recv(); ... } }
//	finally { sub.close(); }
func (h *ClientHdr) Subscribe(rawMatcher map[string]interface{}) *Subscription {
	m, err := MapToMatcher(rawMatcher)
	if err != nil {
		panic(h.vu.Runtime().NewGoError(err))
	}
	return &Subscription{streamHandle: newStreamHandle(h, m)}
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
	fn, isFn := sobek.AssertFunction(cb)
	if !isFn {
		panic(h.vu.Runtime().NewGoError(errors.New("Serve: cb must be a function")))
	}
	m, err := MapToMatcher(rawMatcher)
	if err != nil {
		panic(h.vu.Runtime().NewGoError(err))
	}
	sh := &ServeHandle{streamHandle: newStreamHandle(h, m)}
	go sh.run(fn)
	return sh
}

// streamHandle is the shared state and lifecycle behind Subscription
// (chan-like recv) and ServeHandle (callback). All exported handles
// embed it and share Close.
type streamHandle struct {
	hdr    *ClientHdr
	sub    *serveSub
	done   chan struct{}
	closed atomic.Bool
}

func newStreamHandle(h *ClientHdr, m Matcher) *streamHandle {
	return &streamHandle{
		hdr:  h,
		sub:  h.Client.disp.registerServe(m),
		done: make(chan struct{}),
	}
}

// Close unregisters from the dispatcher and cancels any consumer
// waiting on the handle. Idempotent.
func (s *streamHandle) Close() {
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	close(s.done)
	s.hdr.Client.disp.unregisterServe(s.sub)
}

// Subscription is a JS-facing handle for a Subscribe stream.
type Subscription struct{ *streamHandle }

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

// ServeHandle is a JS-facing handle for a Serve stream.
type ServeHandle struct{ *streamHandle }

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
			enqueue(func() error {
				_, err := cb(sobek.Undefined(), rt.ToValue(msg))
				return err
			})
		}
	}
}
