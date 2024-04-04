/*
example stress test for AIR and ULR
*/
import { check } from 'k6';
import diameter from 'k6/x/diameter';

let client;

export default function () {
    if (client == null) {
        client = new diameter.K6DiameterClient();
    }
    try {
        const result = client.connect({
            addr: `127.0.0.1:3868`,
            host: "magma-oai.openair4G.eur",
            realm: "openair4G.eur",
            network_type: "sctp",
            retries: 0,
            vendor_id: 10415,
        });
        check(result, {
            'Connected': (result) => result == true
        })
    } catch (error) {
        check(null, {
            'Connected': false,
        });
        return;
    }
    for (let i = 0; i < 4096; i++) {
        try {
            const airRes = client.checkSendAIR({
                ueimsi: "00" + (1010000000001 + i),
                plmn_id: "\x05",
                vectors: 3,
                completion_sleep: 5,
            });
            check(airRes, {
                'Received AIR Response': (airRes) => airRes == true,
            });
        } catch (error) {
            check(null, {
                'Received AIR Response': false,
            });
        }
        try {
            const ulrRes = client.checkSendULR({
                ueimsi: "00" + (1010000000001 + i),
                plmn_id: "\x05",
                vectors: 3,
                completion_sleep: 5,
            });
            check(ulrRes, {
                'Received ULR Response': (ulrRes) => ulrRes == true
            });
        } catch (error) {

            check(null, {
                'Received ULR Response': false
            });
        }
    }
    client.close();
}
