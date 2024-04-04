package diameter

import (
	"log"
	"math/rand"
	"net"
	"strconv"
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
	RootModule struct{}

	// ModuleInstance represents an instance of the GRPC module for every VU.
	ModuleInstance struct {
		Version string
		vu      modules.VU
		exports map[string]interface{}
	}
)

var (
	_ modules.Module   = &RootModule{}
	_ modules.Instance = &ModuleInstance{}
)

// NewModuleInstance implements the modules.Module interface to return
// a new instance for each VU.
func (*RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	mi := &ModuleInstance{
		Version: version,
		vu:      vu,
		exports: make(map[string]interface{}),
	}
	mi.exports["K6DiameterClient"] = mi.NewK6DiameterClient
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
	AppId           uint
	Ueimsi          string
	PlmnID          string
	Vectors         uint
	CompletionSleep uint
}

type K6DiameterClient struct {
	vu              modules.VU
	cfg             *sm.Settings
	Conn            diam.Conn
	handlerChannels handlerChannels
}

type handlerChannels struct {
	checkAIR chan error
	checkULR chan error
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
	cfg := &sm.Settings{
		OriginHost:       datatype.DiameterIdentity(options.Host),
		OriginRealm:      datatype.DiameterIdentity(options.Realm),
		VendorID:         datatype.Unsigned32(options.VendorId),
		ProductName:      "xk6-diameter",
		OriginStateID:    datatype.Unsigned32(time.Now().Unix()),
		FirmwareRevision: 1,
		HostIPAddresses: []datatype.Address{
			datatype.Address(net.ParseIP("127.0.0.1")),
		},
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
	c.handlerChannels.checkAIR = make(chan error, 1000)
	mux.HandleIdx(
		diam.CommandIndex{AppID: diam.TGPP_S6A_APP_ID, Code: diam.AuthenticationInformation, Request: false},
		handleAuthenticationInformationAnswer(c.handlerChannels.checkAIR))

	c.handlerChannels.checkULR = make(chan error, 1000)
	mux.HandleIdx(
		diam.CommandIndex{AppID: diam.TGPP_S6A_APP_ID, Code: diam.UpdateLocation, Request: false},
		handleUpdateLocationAnswer(c.handlerChannels.checkULR))

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

func (c *K6DiameterClient) SendAIR(options ConnectionOptions) (bool, error) {
	meta, ok := smpeer.FromContext(c.Conn.Context())
	if !ok {
		return false, errors.New("peer metadata unavailable")
	}

	sid := "session;" + strconv.Itoa(int(rand.Uint32()))
	m := diam.NewRequest(diam.AuthenticationInformation, diam.TGPP_S6A_APP_ID, dict.Default)
	m.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String(sid))
	m.NewAVP(avp.OriginHost, avp.Mbit, 0, c.cfg.OriginHost)
	m.NewAVP(avp.OriginRealm, avp.Mbit, 0, c.cfg.OriginRealm)
	m.NewAVP(avp.DestinationRealm, avp.Mbit, 0, meta.OriginRealm)
	m.NewAVP(avp.DestinationHost, avp.Mbit, 0, meta.OriginHost)
	m.NewAVP(avp.UserName, avp.Mbit, 0, datatype.UTF8String(options.Ueimsi))
	m.NewAVP(avp.AuthSessionState, avp.Mbit, 0, datatype.Enumerated(0))
	m.NewAVP(avp.VisitedPLMNID, avp.Vbit|avp.Mbit, uint32(options.VendorId), datatype.OctetString(options.PlmnID))
	m.NewAVP(avp.RequestedEUTRANAuthenticationInfo, avp.Vbit|avp.Mbit, uint32(options.VendorId), &diam.GroupedAVP{
		AVP: []*diam.AVP{
			diam.NewAVP(
				avp.NumberOfRequestedVectors, avp.Vbit|avp.Mbit, uint32(options.VendorId), datatype.Unsigned32(options.Vectors)),
			diam.NewAVP(
				avp.ImmediateResponsePreferred, avp.Vbit|avp.Mbit, uint32(options.VendorId), datatype.Unsigned32(0)),
		},
	})
	if _, err := m.WriteTo(c.Conn); err != nil {
		return false, errors.WithMessage(err, "write message fail")
	}

	return true, nil
}

func (c *K6DiameterClient) CheckSendAIR(options ConnectionOptions) (bool, error) {
	if _, err := c.SendAIR(options); err != nil {
		return false, err
	}
	select {
	case res := <-c.handlerChannels.checkAIR:
		if res != nil {
			return false, errors.New("Authentication Information Parse Error")
		}
	case <-time.After(time.Duration(options.CompletionSleep) * time.Second):
		return false, errors.New("Authentication Information timeout")
	}
	return true, nil
}

func (c *K6DiameterClient) SendULR(options ConnectionOptions) (bool, error) {
	meta, ok := smpeer.FromContext(c.Conn.Context())
	if !ok {
		return false, errors.New("peer metadata unavailable")
	}
	sid := "session;" + strconv.Itoa(int(rand.Uint32()))
	m := diam.NewRequest(diam.UpdateLocation, diam.TGPP_S6A_APP_ID, dict.Default)
	m.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String(sid))
	m.NewAVP(avp.OriginHost, avp.Mbit, 0, c.cfg.OriginHost)
	m.NewAVP(avp.OriginRealm, avp.Mbit, 0, c.cfg.OriginRealm)
	m.NewAVP(avp.DestinationRealm, avp.Mbit, 0, meta.OriginRealm)
	m.NewAVP(avp.DestinationHost, avp.Mbit, 0, meta.OriginHost)
	m.NewAVP(avp.UserName, avp.Mbit, 0, datatype.UTF8String(options.Ueimsi))
	m.NewAVP(avp.AuthSessionState, avp.Mbit, 0, datatype.Enumerated(0))
	m.NewAVP(avp.RATType, avp.Mbit, uint32(options.VendorId), datatype.Enumerated(1004))
	m.NewAVP(avp.ULRFlags, avp.Vbit|avp.Mbit, uint32(options.VendorId), datatype.Unsigned32(ULR_FLAGS))
	m.NewAVP(avp.VisitedPLMNID, avp.Vbit|avp.Mbit, uint32(options.VendorId), datatype.OctetString(options.PlmnID))
	if _, err := m.WriteTo(c.Conn); err != nil {
		return false, errors.WithMessage(err, "write message fail")
	}

	return true, nil
}

func (c *K6DiameterClient) CheckSendULR(options ConnectionOptions) (bool, error) {
	if _, err := c.SendULR(options); err != nil {
		return false, err
	}
	select {
	case res := <-c.handlerChannels.checkULR:
		if res != nil {
			return false, errors.New("Authentication Information Parse Error")
		}
	case <-time.After(3 * time.Second):
		return false, errors.New("Authentication Information timeout")
	}
	return true, nil
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

func handleAuthenticationInformationAnswer(done chan error) diam.HandlerFunc {
	return func(c diam.Conn, m *diam.Message) {
		var aia AIA
		err := m.Unmarshal(&aia)
		if err != nil {
			done <- errors.WithMessage(err, "AIA Unmarshal failed")
			return
		}
		done <- nil
	}
}

func handleUpdateLocationAnswer(done chan error) diam.HandlerFunc {
	return func(c diam.Conn, m *diam.Message) {
		var ula ULA
		err := m.Unmarshal(&ula)
		if err != nil {
			done <- errors.WithMessage(err, "ULA Unmarshal failed")
			return
		}
		done <- nil
	}
}

func handleAll() diam.HandlerFunc {
	return func(c diam.Conn, m *diam.Message) {
		log.Printf("Received Meesage From %s\n%s\n", c.RemoteAddr(), m)
	}
}
