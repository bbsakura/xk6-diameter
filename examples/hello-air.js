/*
Minimal S6a AIR example.

Sends a single Authentication-Information-Request to an HSS peer and
asserts the peer replies with DIAMETER_SUCCESS (2001). Adjust `connOpts`
to point at your Diameter server. `./out/bin/hss-server` (built with
`make build`) is one option for a local peer.
*/
import { check } from "k6";
import diameter from "k6/x/diameter";

const connOpts = {
    addr: "127.0.0.1:3868",
    host: "mme.example",
    realm: "example.org",
    network_type: "tcp",
    vendor_id: 10415,
    product_name: "xk6-diameter",
    hostipaddresses: ["127.0.0.1"],
};

const conn = diameter.EnsureConn("hss", connOpts);

export const options = {
    vus: 1,
    iterations: 1,
    tags: { name: "diameter" },
};

export default function () {
    const resultCode = conn.checkSendAIR({
        completion_sleep: 5,
        destination_realm: "example.org",
        additional: [
            { key: "Auth-Session-State", value: 0 },
            { key: "User-Name", value: "001010000000001" },
            { key: "Visited-PLMN-Id", value: [0x05] },
            {
                key: "Requested-EUTRAN-Authentication-Info",
                value: [
                    { key: "Number-Of-Requested-Vectors", value: 1 },
                    { key: "Immediate-Response-Preferred", value: 0 },
                ],
            },
        ],
    });

    check(resultCode, {
        "AIA Result-Code == 2001": (rc) => rc === 2001,
    });
}
