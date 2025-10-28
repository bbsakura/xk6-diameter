
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
        return 1;
     } catch (e) {
         if (e.message.includes("i/o timeout")) {
                return 0;
            }
        return e;
    }

}
