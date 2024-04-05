package diameter

import (
	"github.com/fiorix/go-diameter/v4/diam/avp"
)

var avpDict map[string]*AvpMeta

const vendorId3GPP = 10415

func init() {
	// TODO: generate this part from XML dictionary files
	avpDict = map[string]*AvpMeta{
		"User-Name":                            {code: avp.UserName, flag: avp.Mbit, vendor: 0, converter: toUTF8String},
		"Auth-Session-State":                   {code: avp.AuthSessionState, flag: avp.Mbit, vendor: 0, converter: toEnumerated},
		"Visited-PLMN-Id":                      {code: avp.VisitedPLMNID, flag: avp.Vbit | avp.Mbit, vendor: vendorId3GPP, converter: toOctetString},
		"Requested-EUTRAN-Authentication-Info": {code: avp.RequestedEUTRANAuthenticationInfo, flag: avp.Vbit | avp.Mbit, vendor: vendorId3GPP, converter: toGroupd},
		"Number-Of-Requested-Vectors":          {code: avp.NumberOfRequestedVectors, flag: avp.Vbit | avp.Mbit, vendor: vendorId3GPP, converter: toUnsigned32},
		"Immediate-Response-Preferred":         {code: avp.ImmediateResponsePreferred, flag: avp.Vbit | avp.Mbit, vendor: vendorId3GPP, converter: toUnsigned32},
		"RAT-Type":                             {code: avp.RATType, flag: avp.Vbit | avp.Mbit, vendor: vendorId3GPP, converter: toEnumerated},
		"ULR-Flags":                            {code: avp.ULRFlags, flag: avp.Vbit | avp.Mbit, vendor: vendorId3GPP, converter: toEnumerated},
	}
}
