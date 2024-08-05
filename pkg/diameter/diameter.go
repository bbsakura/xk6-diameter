package diameter

import (
	"log"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/pkg/errors"
	"go.k6.io/k6/js/modules"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm"
	"github.com/fiorix/go-diameter/v4/diam/sm/smpeer"
)

const version = "v0.0.1"

type (
	// RootModule is the global module instance that will create module
	// instances for each VU.
	RootModule struct {
		dialPool *sync.Map
		mu       sync.Mutex
	}

	// ModuleInstance represents an instance of the GRPC module for every VU.
	ModuleInstance struct {
		Version string
		vu      modules.VU
		exports map[string]interface{}
		rm      *RootModule
	}
)

var (
	_ modules.Module   = &RootModule{}
	_ modules.Instance = &ModuleInstance{}
)

func New() *RootModule {
	return &RootModule{
		dialPool: new(sync.Map),
	}
}

// NewModuleInstance implements the modules.Module interface to return
// a new instance for each VU.
func (rm *RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	mi := &ModuleInstance{
		Version: version,
		vu:      vu,
		exports: make(map[string]interface{}),
		rm:      rm,
	}
	mi.exports["K6DiameterClient"] = mi.NewK6DiameterClient
	mi.exports["K6DiameterClientWithConnect"] = mi.NewK6DiameterClientWithConnect
	return mi
}

// Exports implements the modules.Instance interface and returns the exports
// of the JS module.
func (mi *ModuleInstance) Exports() modules.Exports {
	return modules.Exports{
		Named: mi.exports,
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
}

type K6DiameterClient struct {
	vu              modules.VU
	cfg             *sm.Settings
	Conn            diam.Conn
	handlerChannels handlerChannels
}

type handlerChannels struct {
	checkAIR chan AIAResponce
	checkULR chan ULAResponce
	checkCLA chan CLAResponce
}

func (c *ModuleInstance) NewK6DiameterClientWithConnect(call goja.ConstructorCall) *goja.Object {
	c.rm.mu.Lock()
	defer c.rm.mu.Unlock()
	op := call.Arguments[0].Export()
	options, err := MapToConnectionOptions(op.(map[string]interface{}))
	if err != nil {
		panic(err)
	}
	cli := c.rm.connGetPool(options.Host)
	if cli == nil {
		cli = &K6DiameterClient{
			vu: c.vu,
		}
		_, err := cli.Connect(options)
		if err != nil {
			panic(err)
		}
		c.rm.connSetPool(options.Host, cli)
	}
	rt := c.vu.Runtime()
	return rt.ToValue(cli).ToObject(rt)
}

func (c *RootModule) connSetPool(host string, diam *K6DiameterClient) {
	c.dialPool.Store(host, diam)
}

func (c *RootModule) connGetPool(host string) *K6DiameterClient {
	if diam, ok := c.dialPool.Load(host); ok {
		return diam.(*K6DiameterClient)
	}
	return nil
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

func (c *ModuleInstance) NewK6DiameterClient(call goja.ConstructorCall) *goja.Object {
	rt := c.vu.Runtime()
	cli := &K6DiameterClient{
		vu: c.vu,
	}
	return rt.ToValue(cli).ToObject(rt)
}

func (c *K6DiameterClient) Connect(options ConnectionOptions) (bool, error) {
	if len(options.Addr) == 0 {
		return false, errors.New("missing addr")
	}
	hostIPAddresses := []datatype.Address{}
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

	cli := &sm.Client{
		Dict:             dict.Default,
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

	conn, err := cli.DialNetwork(options.NetworkType, options.Addr)
	if err != nil {
		return false, errors.WithMessage(err, "Dial error")
	}
	// set MessageHandler
	c.handlerChannels.checkAIR = make(chan AIAResponce, 1000)
	mux.HandleIdx(
		diam.CommandIndex{AppID: diam.TGPP_S6A_APP_ID, Code: diam.AuthenticationInformation, Request: false},
		handleAuthenticationInformationAnswer(c.handlerChannels.checkAIR))

	c.handlerChannels.checkULR = make(chan ULAResponce, 1000)
	mux.HandleIdx(
		diam.CommandIndex{AppID: diam.TGPP_S6A_APP_ID, Code: diam.UpdateLocation, Request: false},
		handleUpdateLocationAnswer(c.handlerChannels.checkULR))

	c.handlerChannels.checkCLA = make(chan CLAResponce, 1000)

	mux.HandleIdx(diam.CommandIndex{AppID: diam.TGPP_S6A_APP_ID, Code: diam.CancelLocation, Request: true}, handleCancelLocationAnswer(c.handlerChannels.checkCLA))
	// Catch All
	mux.HandleIdx(diam.ALL_CMD_INDEX, handleAll())

	c.Conn = conn
	c.cfg = cfg
	return true, nil
}

func (c *K6DiameterClient) Close() {
	if c.Conn == nil {
		return
	}
	c.Conn.Close()
}

func (c *K6DiameterClient) generateSessionID() string {
	return "session;" + strconv.Itoa(int(rand.Uint32()))
}

func (c *K6DiameterClient) SendAIR(options ConnectionOptions) (bool, error) {
	var err error
	meta, ok := smpeer.FromContext(c.Conn.Context())
	if !ok {
		return false, errors.New("peer metadata unavailable")
	}

	var sid string
	if options.SessionID != "" {
		sid = options.SessionID
	} else {
		sid = c.generateSessionID()
	}
	m := diam.NewRequest(diam.AuthenticationInformation, diam.TGPP_S6A_APP_ID, dict.Default)
	avps := []AVPMeta{
		{code: avp.SessionID, flag: avp.Mbit, vendor: 0, value: datatype.UTF8String(sid)},
		{code: avp.OriginHost, flag: avp.Mbit, vendor: 0, value: c.cfg.OriginHost},
		{code: avp.OriginRealm, flag: avp.Mbit, vendor: 0, value: c.cfg.OriginRealm},
	}
	for _, avp := range avps {
		_, err = m.NewAVP(avp.code, avp.flag, avp.vendor, avp.value)
		if err != nil {
			return false, errors.WithMessage(err, "NewAVP failed")
		}
	}
	if options.ProxiableFlag {
		m.Header.CommandFlags |= diam.ProxiableFlag
	}
	err = modifyMessage(m, meta, options)
	if err != nil {
		log.Println(err)
	}
	err = appendAVPs(m, meta, options.Additional)
	if err != nil {
		log.Println(err)
	}

	if _, err := m.WriteTo(c.Conn); err != nil {
		return false, errors.WithMessage(err, "write message fail")
	}

	return true, nil
}

func (c *K6DiameterClient) CheckSendAIR(options ConnectionOptions) (int64, error) {
	if _, err := c.SendAIR(options); err != nil {
		return 0, err
	}
	select {
	case res := <-c.handlerChannels.checkAIR:
		if res.Error != nil {
			return 0, res.Error
		}
		return int64(res.AIA.ResultCode), nil
	case <-time.After(time.Duration(options.CompletionSleep) * time.Second):
		return 0, errors.New("Authentication Information timeout")
	}
}

func (c *K6DiameterClient) SendULR(options ConnectionOptions) (bool, error) {
	var err error
	meta, ok := smpeer.FromContext(c.Conn.Context())
	if !ok {
		return false, errors.New("peer metadata unavailable")
	}
	var sid string
	if options.SessionID != "" {
		sid = options.SessionID
	} else {
		sid = c.generateSessionID()
	}
	m := diam.NewRequest(diam.UpdateLocation, diam.TGPP_S6A_APP_ID, dict.Default)
	avps := []AVPMeta{
		{code: avp.SessionID, flag: avp.Mbit, vendor: 0, value: datatype.UTF8String(sid)},
		{code: avp.OriginHost, flag: avp.Mbit, vendor: 0, value: c.cfg.OriginHost},
		{code: avp.OriginRealm, flag: avp.Mbit, vendor: 0, value: c.cfg.OriginRealm},
	}
	for _, avp := range avps {
		_, err = m.NewAVP(avp.code, avp.flag, avp.vendor, avp.value)
		if err != nil {
			return false, errors.WithMessage(err, "NewAVP failed")
		}
	}
	if options.ProxiableFlag {
		m.Header.CommandFlags |= diam.ProxiableFlag
	}
	err = modifyMessage(m, meta, options)
	if err != nil {
		log.Println(err)
	}
	err = appendAVPs(m, meta, options.Additional)
	if err != nil {
		log.Println(err)
	}

	if _, err := m.WriteTo(c.Conn); err != nil {
		return false, errors.WithMessage(err, "write message fail")
	}

	return true, nil
}

func (c *K6DiameterClient) CheckSendULR(options ConnectionOptions) (int64, error) {
	if _, err := c.SendULR(options); err != nil {
		return 0, err
	}
	select {
	case res := <-c.handlerChannels.checkULR:
		if res.Error != nil {
			return 0, res.Error
		}
		return int64(res.ULA.ResultCode), nil
	case <-time.After(time.Duration(options.CompletionSleep) * time.Second):
		return 0, errors.New("Authentication Information timeout")
	}
}

func (c *K6DiameterClient) CheckCLA(wait int64) (int64, error) {
	select {
	case res := <-c.handlerChannels.checkCLA:
		if res.Error != nil {
			return 0, res.Error
		}
		return int64(res.CLA.ResultCode), nil
	case <-time.After(time.Duration(wait) * time.Second):
		return 0, errors.New("Cancel Location timeout")
	}
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

func handleAuthenticationInformationAnswer(done chan AIAResponce) diam.HandlerFunc {
	return func(c diam.Conn, m *diam.Message) {
		var aia AIA
		err := m.Unmarshal(&aia)
		if err != nil {
			done <- AIAResponce{Error: errors.WithMessage(err, "AIA Unmarshal failed")}
			return
		}
		done <- AIAResponce{AIA: aia, Error: nil}
	}
}

func handleUpdateLocationAnswer(done chan ULAResponce) diam.HandlerFunc {
	return func(c diam.Conn, m *diam.Message) {
		var ula ULA
		err := m.Unmarshal(&ula)
		if err != nil {
			done <- ULAResponce{Error: errors.WithMessage(err, "ULA Unmarshal failed")}
			return
		}
		done <- ULAResponce{ULA: ula, Error: nil}
	}
}

func handleCancelLocationAnswer(done chan CLAResponce) diam.HandlerFunc {
	return func(c diam.Conn, m *diam.Message) {
		var cla CLA
		err := m.Unmarshal(&cla)
		if err != nil {
			done <- CLAResponce{Error: errors.WithMessage(err, "CLA Unmarshal failed")}
			return
		}
		done <- CLAResponce{CLA: cla, Error: nil}
	}
}

func handleAll() diam.HandlerFunc {
	return func(c diam.Conn, m *diam.Message) {
		log.Printf("Received Meesage From %s\n%s\n", c.RemoteAddr(), m)
	}
}
