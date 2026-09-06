package diameter

import (
	"log"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/grafana/sobek"
	"github.com/pkg/errors"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/js/promises"
	"go.k6.io/k6/metrics"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm"
	"github.com/fiorix/go-diameter/v4/diam/sm/smpeer"
)

type (
	// RootModule is the singleton module instance shared across all VUs.
	// namedPool caches *Client by user-supplied name (EnsureConn);
	// idPool caches *Client by its auto-assigned UUID v7 (GetConn).
	// A Conn dialed via EnsureConn ends up in both pools; one dialed via
	// NewConn ends up in idPool only.
	RootModule struct {
		namedPool *sync.Map // string → *Client
		idPool    *sync.Map // uuid.UUID → *Client
		mu        sync.Mutex
		once      sync.Once
		tags      *metrics.TagSet
	}

	// ModuleInstance represents an instance of the module for every VU.
	ModuleInstance struct {
		vu       modules.VU
		rm       *RootModule
		mLatency *metrics.Metric
	}
)

var (
	_ modules.Module   = &RootModule{}
	_ modules.Instance = &ModuleInstance{}
)

func New() *RootModule {
	return &RootModule{
		namedPool: new(sync.Map),
		idPool:    new(sync.Map),
	}
}

// NewModuleInstance implements the modules.Module interface to return
// a new instance for each VU.
func (rm *RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	reg := vu.InitEnv().Registry
	rm.once.Do(func() {
		rm.tags = reg.RootTagSet().With("module", "diameter")
	})
	return &ModuleInstance{
		vu:       vu,
		rm:       rm,
		mLatency: reg.MustNewMetric("diameter_tx_duration", metrics.Trend, metrics.Time),
	}
}

// Exports implements the modules.Instance interface and returns the exports
// of the JS module.
func (mi *ModuleInstance) Exports() modules.Exports {
	return modules.Exports{
		Named: map[string]interface{}{
			"Conn":       mi.NewConn,
			"EnsureConn": mi.EnsureConn,
			"GetConn":    mi.GetConn,
		},
	}
}

type ConnectionOptions struct {
	Addr            string
	Host            string
	Realm           string
	NetworkType     string
	Retries         uint
	VendorId        uint
	ProductName     string
	HostIPAddresses []string
	AppId           uint
	Ueimsi          string
	PlmnID          string
	Vectors         uint
	CompletionSleep uint
	SessionID       string

	DestinationHost  *datatype.DiameterIdentity
	DestinationRealm *datatype.DiameterIdentity

	ProxiableFlag bool
	Additional    []AVP

	// Dict overrides dict.Default for both sm.Client and outgoing
	// Requests. Nil falls back to dict.Default (existing behavior).
	Dict *dict.Parser
}

// Request describes an arbitrary Diameter command. Use this with
// Client.SendRequest / CheckSendRequest / ClientHdr.SendRequest to
// send messages beyond the built-in AIR/ULR wrappers. AVPs are added
// to the message in order; unlike the AIR/ULR path, no Session-Id /
// Origin-Host / Origin-Realm / Destination-* AVPs are auto-injected —
// the caller is responsible for supplying the full AVP set.
type Request struct {
	AppID           uint32
	Cmd             uint32
	Flags           uint8 // OR'd into m.Header.CommandFlags after RequestFlag
	AVPs            []AVP
	CompletionSleep uint // seconds; 0 = no wait budget → immediate timeout
}

// Client is the shared, VU-independent Diameter resource. It owns the
// connection, peer settings, and the correlation table used to route
// client-initiated Answers back to their Requests. Do not embed a
// modules.VU here — a single Client can be shared across VUs via the
// pools on RootModule.
type Client struct {
	id   uuid.UUID
	cfg  *sm.Settings
	Conn diam.Conn
	dict *dict.Parser
	corr *correlationTbl
	disp *dispatcher
}

// ID returns the Client's UUID v7 string — the key used by idPool /
// GetConn to look this Client back up across VUs.
func (c *Client) ID() string { return c.id.String() }

// Dict returns the dictionary attached to this Client. Callers building
// Diameter messages directly (e.g. via diam.NewRequest) can pass this
// so the message shares the peer's AVP name resolution.
func (c *Client) Dict() *dict.Parser {
	if c.dict != nil {
		return c.dict
	}
	return dict.Default
}

// ClientHdr is a per-VU handle that references a shared *Client and
// carries VU-scoped state (VU handle + metric handles) so that
// transaction latency can be attributed to the calling VU. It is the
// type exposed to JS by all constructors below.
type ClientHdr struct {
	Client   *Client
	vu       modules.VU
	mLatency *metrics.Metric
	tags     *metrics.TagSet
}

// correlationTbl routes client-initiated Answers to the goroutine
// waiting for them, keyed by Hop-by-Hop Identifier (RFC 6733 §3).
type correlationTbl struct {
	mu      sync.Mutex
	pending map[uint32]chan *diam.Message
}

func newCorrelationTbl() *correlationTbl {
	return &correlationTbl{pending: make(map[uint32]chan *diam.Message)}
}

func (t *correlationTbl) register(hbhID uint32) chan *diam.Message {
	ch := make(chan *diam.Message, 1)
	t.mu.Lock()
	t.pending[hbhID] = ch
	t.mu.Unlock()
	return ch
}

func (t *correlationTbl) unregister(hbhID uint32) {
	t.mu.Lock()
	delete(t.pending, hbhID)
	t.mu.Unlock()
}

// deliver hands m to the pending channel for its HBH-ID. Non-blocking:
// a duplicate Answer or one arriving after Unregister is silently
// dropped so the mux reader goroutine cannot stall on a full channel.
func (t *correlationTbl) deliver(m *diam.Message) bool {
	t.mu.Lock()
	ch, ok := t.pending[m.Header.HopByHopID]
	t.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- m:
		return true
	default:
		return false
	}
}

func (c *ModuleInstance) newClientHdr(cli *Client) *ClientHdr {
	return &ClientHdr{
		Client:   cli,
		vu:       c.vu,
		mLatency: c.mLatency,
		tags:     c.rm.tags,
	}
}

func MapToConnectionOptions(m map[string]interface{}) (ConnectionOptions, error) {
	var co ConnectionOptions

	if addr, ok := m["addr"].(string); ok {
		co.Addr = addr
	}
	if host, ok := m["host"].(string); ok {
		co.Host = host
	}
	if realm, ok := m["realm"].(string); ok {
		co.Realm = realm
	}
	if networkType, ok := m["network_type"].(string); ok {
		co.NetworkType = networkType
	}

	mapNumberToUintOpt(&co.Retries, m, "retries")
	mapNumberToUintOpt(&co.VendorId, m, "vendor_id")
	mapNumberToUintOpt(&co.AppId, m, "app_id")
	mapNumberToUintOpt(&co.Vectors, m, "vectors")
	mapNumberToUintOpt(&co.CompletionSleep, m, "completion_sleep")

	if productName, ok := m["product_name"].(string); ok {
		co.ProductName = productName
	}
	if hostIPAddresses, ok := m["hostipaddresses"].([]interface{}); ok {
		for _, ip := range hostIPAddresses {
			if ipStr, ok := ip.(string); ok {
				co.HostIPAddresses = append(co.HostIPAddresses, ipStr)
			}
		}
	}
	if ueimsi, ok := m["ueimsi"].(string); ok {
		co.Ueimsi = ueimsi
	}
	if plmnID, ok := m["plmn_id"].(string); ok {
		co.PlmnID = plmnID
	}
	if sessionID, ok := m["session_id"].(string); ok {
		co.SessionID = sessionID
	}
	if destinationHost, ok := m["destination_host"].(*datatype.DiameterIdentity); ok {
		co.DestinationHost = destinationHost
	}
	if destinationRealm, ok := m["destination_realm"].(*datatype.DiameterIdentity); ok {
		co.DestinationRealm = destinationRealm
	}
	if proxiableFlag, ok := m["proxiable_flag"].(bool); ok {
		co.ProxiableFlag = proxiableFlag
	}
	if additional, ok := m["additional"].([]interface{}); ok {
		for _, avp := range additional {
			if avpCasted, ok := avp.(AVP); ok {
				co.Additional = append(co.Additional, avpCasted)
			}
		}
	}

	return co, nil
}

func mapNumberToUintOpt(target *uint, m map[string]interface{}, key string) {
	if value, ok := m[key].(int64); ok {
		*target = uint(value)
	}
}

// NewConn is the JS constructor for `new diameter.Conn(options)`.
// It dials the peer immediately, registers the *Client in idPool, and
// exposes the UUID v7 key on the returned object as `.id` so JS can
// pass it to GetConn later.
func (c *ModuleInstance) NewConn(call sobek.ConstructorCall) *sobek.Object {
	if len(call.Arguments) != 1 {
		panic(errors.Errorf("Conn constructor: expected 1 argument (options), got %d", len(call.Arguments)))
	}
	op, ok := call.Arguments[0].Export().(map[string]interface{})
	if !ok {
		panic(errors.New("Conn constructor: options must be an object"))
	}
	options, err := MapToConnectionOptions(op)
	if err != nil {
		panic(err)
	}
	cli, err := NewClient(options)
	if err != nil {
		panic(err)
	}
	c.rm.idPool.Store(cli.id, cli)
	return c.wrapClient(cli)
}

// EnsureConn returns a ClientHdr wrapping a shared *Client from the
// named pool so multiple VUs reuse a single Diameter connection. If no
// entry exists for name, it dials a new *Client using params and
// registers it in both namedPool and idPool.
func (c *ModuleInstance) EnsureConn(name string, params map[string]interface{}) *sobek.Object {
	c.rm.mu.Lock()
	defer c.rm.mu.Unlock()
	if v, ok := c.rm.namedPool.Load(name); ok {
		return c.wrapClient(v.(*Client))
	}
	options, err := MapToConnectionOptions(params)
	if err != nil {
		panic(err)
	}
	cli, err := NewClient(options)
	if err != nil {
		panic(err)
	}
	c.rm.namedPool.Store(name, cli)
	c.rm.idPool.Store(cli.id, cli)
	return c.wrapClient(cli)
}

// GetConn returns a ClientHdr for a previously registered *Client
// identified by the UUID v7 string exposed on `.id`. Panics if id is
// not a valid UUID or has no live Client.
func (c *ModuleInstance) GetConn(id string) *sobek.Object {
	uid, err := uuid.Parse(id)
	if err != nil {
		panic(errors.WithMessagef(err, "GetConn: invalid uuid %q", id))
	}
	v, ok := c.rm.idPool.Load(uid)
	if !ok {
		panic(errors.Errorf("GetConn: Conn with id %q not found", id))
	}
	return c.wrapClient(v.(*Client))
}

// wrapClient builds a ClientHdr for the current VU and exposes the
// underlying Client's UUID as a JS `.id` property on the returned
// object.
func (c *ModuleInstance) wrapClient(cli *Client) *sobek.Object {
	rt := c.vu.Runtime()
	obj := rt.ToValue(c.newClientHdr(cli)).ToObject(rt)
	_ = obj.Set("id", cli.id.String())
	return obj
}

// NewClient dials the peer immediately and returns a fully-initialized
// Client. Conn is guaranteed non-nil on success; on failure the caller
// receives no partial Client to clean up.
func NewClient(options ConnectionOptions) (*Client, error) {
	if len(options.Addr) == 0 {
		return nil, errors.New("missing addr")
	}
	hostIPAddresses := make([]datatype.Address, 0, len(options.HostIPAddresses))
	for _, ip := range options.HostIPAddresses {
		hostIPAddresses = append(hostIPAddresses, datatype.Address(net.ParseIP(ip)))
	}
	cfg := &sm.Settings{
		OriginHost:       datatype.DiameterIdentity(options.Host),
		OriginRealm:      datatype.DiameterIdentity(options.Realm),
		VendorID:         datatype.Unsigned32(options.VendorId),
		ProductName:      datatype.UTF8String(options.ProductName),
		OriginStateID:    datatype.Unsigned32(time.Now().Unix()),
		FirmwareRevision: 1,
		HostIPAddresses:  hostIPAddresses,
	}
	mux := sm.New(cfg)

	id, err := uuid.NewV7()
	if err != nil {
		return nil, errors.WithMessage(err, "uuid.NewV7")
	}
	c := &Client{
		id:   id,
		cfg:  cfg,
		dict: options.Dict, // nil is fine; Dict() falls back to dict.Default
		corr: newCorrelationTbl(),
		disp: newDispatcher(),
	}

	// Every incoming message hits the ALL_CMD_INDEX handler: HBH-ID
	// correlation for pending Send*/CheckSend* first, then the
	// dispatcher (Receive/Serve subscribers). Unclaimed messages drop.
	mux.HandleIdx(diam.ALL_CMD_INDEX, diam.HandlerFunc(func(_ diam.Conn, m *diam.Message) {
		if c.corr.deliver(m) {
			return
		}
		c.disp.dispatch(m)
	}))

	dialer := &sm.Client{
		Dict:             c.Dict(),
		Handler:          mux,
		MaxRetransmits:   options.Retries,
		EnableWatchdog:   false,
		WatchdogInterval: 0,
		SupportedVendorID: []*diam.AVP{
			diam.NewAVP(avp.SupportedVendorID, avp.Mbit, 0, datatype.Unsigned32(options.VendorId)),
		},
		VendorSpecificApplicationID: []*diam.AVP{
			diam.NewAVP(avp.VendorSpecificApplicationID, avp.Mbit, 0, &diam.GroupedAVP{
				AVP: []*diam.AVP{
					diam.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(options.AppId)),
					diam.NewAVP(avp.VendorID, avp.Mbit, 0, datatype.Unsigned32(options.VendorId)),
				},
			}),
		},
	}

	conn, err := dialer.DialNetwork(options.NetworkType, options.Addr)
	if err != nil {
		return nil, errors.WithMessage(err, "Dial error")
	}
	c.Conn = conn
	return c, nil
}

func (c *Client) Close() {
	if c.Conn == nil {
		return
	}
	c.Conn.Close()
}

func (c *Client) generateSessionID() string {
	return "session;" + strconv.Itoa(int(rand.Uint32()))
}

// buildRequest constructs a Diameter request with the standard S6a
// Session-Id / Origin-Host / Origin-Realm AVPs plus the caller-supplied
// Additional AVPs. The returned message has its HopByHopID assigned by
// diam.NewRequest and is used by both Send* (fire-and-forget) and
// CheckSend* (pending registration keyed by HopByHopID).
func (c *Client) buildRequest(cmd uint32, options ConnectionOptions) (*diam.Message, error) {
	meta, ok := smpeer.FromContext(c.Conn.Context())
	if !ok {
		return nil, errors.New("peer metadata unavailable")
	}
	sid := options.SessionID
	if sid == "" {
		sid = c.generateSessionID()
	}
	m := diam.NewRequest(cmd, diam.TGPP_S6A_APP_ID, c.Dict())
	for _, a := range []AVPMeta{
		{code: avp.SessionID, flag: avp.Mbit, vendor: 0, value: datatype.UTF8String(sid)},
		{code: avp.OriginHost, flag: avp.Mbit, vendor: 0, value: c.cfg.OriginHost},
		{code: avp.OriginRealm, flag: avp.Mbit, vendor: 0, value: c.cfg.OriginRealm},
	} {
		if _, err := m.NewAVP(a.code, a.flag, a.vendor, a.value); err != nil {
			return nil, errors.WithMessage(err, "NewAVP failed")
		}
	}
	if options.ProxiableFlag {
		m.Header.CommandFlags |= diam.ProxiableFlag
	}
	if err := modifyMessage(m, meta, options); err != nil {
		log.Println(err)
	}
	if err := appendAVPs(m, meta, options.Additional); err != nil {
		log.Println(err)
	}
	return m, nil
}

// buildGeneric constructs a Diameter Request from a Request struct with
// no auto-injected AVPs. The caller supplies the complete AVP list.
func (c *Client) buildGeneric(req Request) (*diam.Message, error) {
	meta, ok := smpeer.FromContext(c.Conn.Context())
	if !ok {
		return nil, errors.New("peer metadata unavailable")
	}
	m := diam.NewRequest(req.Cmd, req.AppID, c.Dict())
	m.Header.CommandFlags |= req.Flags
	if err := appendAVPs(m, meta, req.AVPs); err != nil {
		return nil, errors.WithMessage(err, "appendAVPs failed")
	}
	return m, nil
}

// SendRequest writes an arbitrary Diameter Request to the wire without
// waiting for its Answer. The Answer, if any, is silently dropped by
// the correlation catch-all handler. Use CheckSendRequest or
// ClientHdr.SendRequest to receive the Answer.
func (c *Client) SendRequest(req Request) (bool, error) {
	m, err := c.buildGeneric(req)
	if err != nil {
		return false, err
	}
	if _, err := m.WriteTo(c.Conn); err != nil {
		return false, errors.WithMessage(err, "write message fail")
	}
	return true, nil
}

// CheckSendRequest writes an arbitrary Diameter Request and waits for
// the peer's Answer, correlated by Hop-by-Hop id, up to
// req.CompletionSleep seconds. Returns the raw Answer message so the
// caller can Unmarshal into any struct that matches the peer's dict.
func (c *Client) CheckSendRequest(req Request) (*diam.Message, error) {
	m, err := c.buildGeneric(req)
	if err != nil {
		return nil, err
	}
	hbhID := m.Header.HopByHopID
	ch := c.corr.register(hbhID)
	defer c.corr.unregister(hbhID)

	if _, err := m.WriteTo(c.Conn); err != nil {
		return nil, errors.WithMessage(err, "write message fail")
	}
	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(time.Duration(req.CompletionSleep) * time.Second):
		return nil, errors.Errorf("request timeout (appID=%d cmd=%d)", req.AppID, req.Cmd)
	}
}

func (c *Client) SendAIR(options ConnectionOptions) (bool, error) {
	m, err := c.buildRequest(diam.AuthenticationInformation, options)
	if err != nil {
		return false, err
	}
	if _, err := m.WriteTo(c.Conn); err != nil {
		return false, errors.WithMessage(err, "write message fail")
	}
	return true, nil
}

func (c *Client) CheckSendAIR(options ConnectionOptions) (int64, error) {
	m, err := c.buildRequest(diam.AuthenticationInformation, options)
	if err != nil {
		return 0, err
	}
	hbhID := m.Header.HopByHopID
	ch := c.corr.register(hbhID)
	defer c.corr.unregister(hbhID)

	if _, err := m.WriteTo(c.Conn); err != nil {
		return 0, errors.WithMessage(err, "write message fail")
	}
	select {
	case resp := <-ch:
		var aia AIA
		if err := resp.Unmarshal(&aia); err != nil {
			return 0, errors.WithMessage(err, "AIA Unmarshal failed")
		}
		return int64(aia.ResultCode), nil
	case <-time.After(time.Duration(options.CompletionSleep) * time.Second):
		return 0, errors.New("Authentication Information timeout")
	}
}

func (c *Client) SendULR(options ConnectionOptions) (bool, error) {
	m, err := c.buildRequest(diam.UpdateLocation, options)
	if err != nil {
		return false, err
	}
	if _, err := m.WriteTo(c.Conn); err != nil {
		return false, errors.WithMessage(err, "write message fail")
	}
	return true, nil
}

func (c *Client) CheckSendULR(options ConnectionOptions) (int64, error) {
	m, err := c.buildRequest(diam.UpdateLocation, options)
	if err != nil {
		return 0, err
	}
	hbhID := m.Header.HopByHopID
	ch := c.corr.register(hbhID)
	defer c.corr.unregister(hbhID)

	if _, err := m.WriteTo(c.Conn); err != nil {
		return 0, errors.WithMessage(err, "write message fail")
	}
	select {
	case resp := <-ch:
		var ula ULA
		if err := resp.Unmarshal(&ula); err != nil {
			return 0, errors.WithMessage(err, "ULA Unmarshal failed")
		}
		return int64(ula.ResultCode), nil
	case <-time.After(time.Duration(options.CompletionSleep) * time.Second):
		return 0, errors.New("Update Location timeout")
	}
}

// ClientHdr method set mirrors Client's, except CheckSend* wrap latency
// measurement. Connect is not exposed — Client is dialed at construction.
func (h *ClientHdr) Close() { h.Client.Close() }

// SendAIR returns a JS Promise that resolves with the decoded AIA
// struct when the peer replies (correlated by Hop-by-Hop id), or
// rejects on send failure / CompletionSleep timeout / unmarshal error.
// Transaction latency is emitted on both resolve and reject.
func (h *ClientHdr) SendAIR(options ConnectionOptions) *sobek.Promise {
	return h.sendPromise(
		"AIR",
		time.Duration(options.CompletionSleep)*time.Second,
		func() (*diam.Message, error) {
			return h.Client.buildRequest(diam.AuthenticationInformation, options)
		},
		func(m *diam.Message) (any, error) {
			var aia AIA
			if err := m.Unmarshal(&aia); err != nil {
				return nil, errors.WithMessage(err, "AIA Unmarshal failed")
			}
			return aia, nil
		},
	)
}

func (h *ClientHdr) SendULR(options ConnectionOptions) *sobek.Promise {
	return h.sendPromise(
		"ULR",
		time.Duration(options.CompletionSleep)*time.Second,
		func() (*diam.Message, error) {
			return h.Client.buildRequest(diam.UpdateLocation, options)
		},
		func(m *diam.Message) (any, error) {
			var ula ULA
			if err := m.Unmarshal(&ula); err != nil {
				return nil, errors.WithMessage(err, "ULA Unmarshal failed")
			}
			return ula, nil
		},
	)
}

// SendRequest resolves with the raw *diam.Message Answer so callers
// can Unmarshal into any struct that matches the peer's dictionary.
func (h *ClientHdr) SendRequest(req Request) *sobek.Promise {
	cmdLabel := "cmd_" + strconv.FormatUint(uint64(req.Cmd), 10)
	if dcmd, err := h.Client.Dict().FindCommand(req.AppID, req.Cmd); err == nil {
		cmdLabel = dcmd.Short
	}
	return h.sendPromise(
		cmdLabel,
		time.Duration(req.CompletionSleep)*time.Second,
		func() (*diam.Message, error) { return h.Client.buildGeneric(req) },
		func(m *diam.Message) (any, error) { return m, nil },
	)
}

// sendPromise runs the standard "build → register → write → wait"
// sequence in a background goroutine and returns a Promise for JS.
// build is expected to produce a Request whose HopByHopID is used as
// the correlation key; unmarshal converts the Answer message into the
// value handed to resolve().
func (h *ClientHdr) sendPromise(
	cmdLabel string,
	timeout time.Duration,
	build func() (*diam.Message, error),
	unmarshal func(*diam.Message) (any, error),
) *sobek.Promise {
	startAt := time.Now()
	p, resolve, reject := promises.New(h.vu)

	m, err := build()
	if err != nil {
		h.pushLatency(cmdLabel, startAt, err)
		reject(err)
		return p
	}
	hbhID := m.Header.HopByHopID
	ch := h.Client.corr.register(hbhID)

	if _, err := m.WriteTo(h.Client.Conn); err != nil {
		h.Client.corr.unregister(hbhID)
		wrapped := errors.WithMessage(err, "write message fail")
		h.pushLatency(cmdLabel, startAt, wrapped)
		reject(wrapped)
		return p
	}

	go func() {
		defer h.Client.corr.unregister(hbhID)
		select {
		case resp := <-ch:
			v, err := unmarshal(resp)
			h.pushLatency(cmdLabel, startAt, err)
			if err != nil {
				reject(err)
				return
			}
			resolve(v)
		case <-time.After(timeout):
			err := errors.New(cmdLabel + " timeout")
			h.pushLatency(cmdLabel, startAt, err)
			reject(err)
		}
	}()
	return p
}

func (h *ClientHdr) CheckSendAIR(options ConnectionOptions) (int64, error) {
	startAt := time.Now()
	code, err := h.Client.CheckSendAIR(options)
	h.pushLatency("AIR", startAt, err)
	return code, err
}

func (h *ClientHdr) CheckSendULR(options ConnectionOptions) (int64, error) {
	startAt := time.Now()
	code, err := h.Client.CheckSendULR(options)
	h.pushLatency("ULR", startAt, err)
	return code, err
}

func (h *ClientHdr) pushLatency(cmd string, startAt time.Time, err error) {
	state := h.vu.State()
	if state == nil {
		return
	}
	tags := h.tags.With("command", cmd)
	if err != nil {
		tags = tags.With("error", "true")
	}
	now := time.Now()
	metrics.PushIfNotDone(h.vu.Context(), state.Samples, metrics.Sample{
		TimeSeries: metrics.TimeSeries{Metric: h.mLatency, Tags: tags},
		Time:       now,
		Value:      metrics.D(now.Sub(startAt)),
	})
}

// S6a/S6d-Indicator | Initial-AttachIndicator
const ULR_FLAGS = 1<<1 | 1<<5

type EUtranVector struct {
	RAND  datatype.OctetString `avp:"RAND"`
	XRES  datatype.OctetString `avp:"XRES"`
	AUTN  datatype.OctetString `avp:"AUTN"`
	KASME datatype.OctetString `avp:"KASME"`
}

type ExperimentalResult struct {
	ExperimentalResultCode datatype.Unsigned32 `avp:"Experimental-Result-Code"`
}

type AuthenticationInfo struct {
	EUtranVector EUtranVector `avp:"E-UTRAN-Vector"`
}

type AIA struct {
	SessionID          datatype.UTF8String       `avp:"Session-Id"`
	ResultCode         datatype.Unsigned32       `avp:"Result-Code"`
	OriginHost         datatype.DiameterIdentity `avp:"Origin-Host"`
	OriginRealm        datatype.DiameterIdentity `avp:"Origin-Realm"`
	AuthSessionState   datatype.UTF8String       `avp:"Auth-Session-State"`
	ExperimentalResult ExperimentalResult        `avp:"Experimental-Result"`
	AIs                []AuthenticationInfo      `avp:"Authentication-Info"`
}

type AMBR struct {
	MaxRequestedBandwidthUL uint32 `avp:"Max-Requested-Bandwidth-UL"`
	MaxRequestedBandwidthDL uint32 `avp:"Max-Requested-Bandwidth-DL"`
}

type AllocationRetentionPriority struct {
	PriorityLevel           uint32 `avp:"Priority-Level"`
	PreemptionCapability    int32  `avp:"Pre-emption-Capability"`
	PreemptionVulnerability int32  `avp:"Pre-emption-Vulnerability"`
}

type EPSSubscribedQoSProfile struct {
	QoSClassIdentifier          int32                       `avp:"QoS-Class-Identifier"`
	AllocationRetentionPriority AllocationRetentionPriority `avp:"Allocation-Retention-Priority"`
}

type APNConfiguration struct {
	ContextIdentifier       uint32                  `avp:"Context-Identifier"`
	PDNType                 int32                   `avp:"PDN-Type"`
	ServiceSelection        string                  `avp:"Service-Selection"`
	EPSSubscribedQoSProfile EPSSubscribedQoSProfile `avp:"EPS-Subscribed-QoS-Profile"`
	AMBR                    AMBR                    `avp:"AMBR"`
}

type APNConfigurationProfile struct {
	ContextIdentifier                     uint32           `avp:"Context-Identifier"`
	AllAPNConfigurationsIncludedIndicator int32            `avp:"All-APN-Configurations-Included-Indicator"`
	APNConfiguration                      APNConfiguration `avp:"APN-Configuration"`
}

type SubscriptionData struct {
	MSISDN                        datatype.OctetString    `avp:"MSISDN"`
	AccessRestrictionData         uint32                  `avp:"Access-Restriction-Data"`
	SubscriberStatus              int32                   `avp:"Subscriber-Status"`
	NetworkAccessMode             int32                   `avp:"Network-Access-Mode"`
	AMBR                          AMBR                    `avp:"AMBR"`
	APNConfigurationProfile       APNConfigurationProfile `avp:"APN-Configuration-Profile"`
	SubscribedPeriodicRauTauTimer uint32                  `avp:"Subscribed-Periodic-RAU-TAU-Timer"`
}

type ULA struct {
	SessionID          string                    `avp:"Session-Id"`
	ULAFlags           uint32                    `avp:"ULA-Flags"`
	SubscriptionData   SubscriptionData          `avp:"Subscription-Data"`
	AuthSessionState   int32                     `avp:"Auth-Session-State"`
	ResultCode         uint32                    `avp:"Result-Code"`
	OriginHost         datatype.DiameterIdentity `avp:"Origin-Host"`
	OriginRealm        datatype.DiameterIdentity `avp:"Origin-Realm"`
	ExperimentalResult ExperimentalResult        `avp:"Experimental-Result"`
}

type CLA struct {
	SessionId        string                    `avp:"Session-Id"`
	AuthSessionState int32                     `avp:"Auth-Session-State"`
	ResultCode       uint32                    `avp:"Result-Code"`
	OriginHost       datatype.DiameterIdentity `avp:"Origin-Host"`
	OriginRealm      datatype.DiameterIdentity `avp:"Origin-Realm"`
}

type AIAResponce struct {
	AIA   AIA
	Error error
}
type ULAResponce struct {
	ULA   ULA
	Error error
}

type CLAResponce struct {
	CLA   CLA
	Error error
}
