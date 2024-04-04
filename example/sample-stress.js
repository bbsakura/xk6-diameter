import { check } from 'k6';
import diameter from 'k6/x/diameter';

let client;

export default function () {
    if (client == null) {
        client = new diameter.K6DiameterClient();
    }
    client.connect({
        addr: `127.0.0.1:3868`,
        host: "magma-oai.openair4G.eur",
        realm: "openair4G.eur",
        network_type: "sctp",
        retries: 0,
        vendor_id: 10415,
    });
    for (let i = 0; i < 4096; i++) {
        const airRes = client.checkSendAIR({
            vendor_id: 10415,
            app_id: 16777251,
            ueimsi: "00" + (1010000000001 + i),
            plmn_id: "\x05",
            vectors: 3,
            completion_sleep: 5,
        });
        if (!airRes) {
            check(airRes, {
                'Received AIR Response': (airRes) => airRes == true,
                'Received ULR Response': false,
            });
            continue;
        }
        const ulrRes = client.checkSendULR({
            vendor_id: 10415,
            app_id: 16777251,
            ueimsi: "00" + (1010000000001 + i),
            plmn_id: "\x05",
            vectors: 3,
            completion_sleep: 5,
        });
        check(ulrRes, {
            'Received AIR Response': (airRes) => airRes == true,
            'Received ULR Response': (ulrRes) => ulrRes == true,
        });
    }
    client.close();
}
