/*
Receiving a server-initiated CLR while sending AIR/ULR.

Demonstrates `conn.subscribe(matcher)`: a background loop consumes any
Cancel-Location-Request (server → client) the HSS pushes, while the
default workload keeps sending Authentication-Information-Requests. See
docs/receiving-messages.md for `receive` / `subscribe` / `serve`
semantics.
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
    iterations: 4,
    tags: { name: "diameter" },
};

export default async function () {
    const sub = conn.subscribe({ cmd_code: "CLR", is_request: true });

    const clrLoop = (async () => {
        try {
            for (;;) {
                const clr = await sub.recv();
                console.log(`CLR HBH=${clr.Header.HopByHopID}`);
            }
        } catch (_) {
            // subscription closed or iteration ended — expected
        }
    })();

    const rc = conn.checkSendAIR({
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
    check(rc, { "AIA Result-Code == 2001": (rc) => rc === 2001 });

    sub.close();
    await clrLoop;
}
