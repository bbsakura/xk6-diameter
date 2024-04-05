
/*
example stress test for AIR and ULR
*/
import { check } from "k6";
import diameter from "k6/x/diameter";

let client;

export const options = {
    tags: { name: "diameter" },
};

export default function () {
    if (client == null) {
        client = new diameter.K6DiameterClient();
    }
    try {
        const result = client.connect({
            addr: "127.0.0.1:3868",
            host: "magma-oai.openair4G.eur",
            realm: "openair4G.eur",
            network_type: "sctp",
            retries: 0,
            vendor_id: 10415,
            product_name: "xk6-diameter",
            hostipaddresses: ["127.0.0.1"],
        });
        check(result, {
            "Connected": (result) => result == true,
        });
    } catch (error) {
        check(null, {
            "Connected": false,
        });
        return;
    }
    for (let i = 0; i < 4096; i++) {
        try {
            const airRes = client.checkSendAIR({
                completion_sleep: 5,
                destination_host: "",
                destination_realm: "diameter.example.com",
                additional: [
                    { key: "Auth-Session-State", value: 0 },
                    { key: "User-Name", value: "00" + (1010000000001 + i) },
                    { key: "Visited-PLMN-Id", value: [0x05] },
                    {
                        key: "Requested-EUTRAN-Authentication-Info",
                        value: [
                            { key: "Number-Of-Requested-Vectors", value: 3 },
                            { key: "Immediate-Response-Preferred", value: 0 },
                        ],
                    },
                ],
            });
            check(airRes, {
                "Received AIR Response": (airRes) => airRes == true,
            });
        } catch (error) {
            // If the connection fails or times out, set the check to false
            console.log(error);
            check(null, {
                "Received AIR Response": false,
            });
        }
        try {
            const ulrRes = client.checkSendULR({
                completion_sleep: 5,
                destination_host: "",
                destination_realm: "diameter.example.com",
                additional: [
                    { key: "Auth-Session-State", value: 0 },
                    { key: "User-Name", value: "00" + (1010000000001 + i) },
                    { key: "RAT-Type", value: 1004 },
                    { key: "ULR-Flags", value: 0b100010 },
                    { key: "Visited-PLMN-Id", value: [0x05] },
                    {
                        key: "Requested-EUTRAN-Authentication-Info",
                        value: [
                            { key: "Number-Of-Requested-Vectors", value: 3 },
                            { key: "Immediate-Response-Preferred", value: 0 },
                        ],
                    },
                ],
            });
            check(ulrRes, {
                "Received ULR Response": (ulrRes) => ulrRes == true,
            });
        } catch (error) {
            // If the connection fails or times out, set the check to false
            console.log(error);
            check(null, {
                "Received ULR Response": false,
            });
        }
    }
    client.close();
}
