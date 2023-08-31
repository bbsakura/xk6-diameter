import { check } from 'k6';


import diameter from 'k6/x/diameter';

let client;

export default function () {
    if (client == null) {
        client = new diameter.K6DiameterClient();
    }
    const connectRes = client.connect({
        addr: `127.0.0.1:3868`,
        host: "magma-oai.openair4G.eur",
        realm: "openair4G.eur",
        networktype: "sctp",
        retries: 0,
        vendorID: 10415,
        appID: 16777251,
        ueIMSI: "001010000000001",
        plmnID: "\x05",
        vectors: 3,
        completionsleep: 10,
    });
    for (let i = 0; i < 4096; i++) {
        const airRes = client.sendAIR();
        if (!airRes) {
            check(airRes, {
                'Received AIR Response': (airRes) => airRes == true,
                'Received ULR Response': false,
            });
            continue;
        }
        const ulrRes = client.sendULR();
        check(ulrRes, {
            'Received AIR Response': (airRes) => airRes == true,
            'Received ULR Response': (ulrRes) => ulrRes == true,
        });
    }
    client.close();
}
