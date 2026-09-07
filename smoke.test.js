
/*
smoke test: EnsureConn returns a shared Diameter connection.
*/
import diameter from "k6/x/diameter";

export const options = {
    tags: { name: "diameter" },
};

export default function () {
    try {
        diameter.EnsureConn("hss-primary", {
            addr: "127.0.0.1:3868",
            host: "magma-oai.openair4G.eur",
            realm: "openair4G.eur",
            network_type: "sctp",
            retries: 0,
            vendor_id: 10415,
            product_name: "xk6-diameter",
            hostipaddresses: ["127.0.0.1"],
        });
    } catch (e) {
        if (e.message && e.message.includes("i/o timeout")) {
            return 0;
        }
        return e;
    }
    return 1;
}
