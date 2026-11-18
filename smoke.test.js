/*
smoke test: verify the k6/x/diameter module loads and exposes its API.

xk6 lint runs this without a peer available, so the test intentionally
avoids dialing any Diameter endpoint. A functional example that dials a
real HSS lives at examples/air-ulr-stress.js.
*/
import diameter from "k6/x/diameter";
import { check } from "k6";

export const options = {
    vus: 1,
    iterations: 1,
    tags: { name: "diameter" },
    thresholds: {
        checks: ["rate==1"],
    },
};

function getConnThrowsOnInvalidId() {
    try {
        diameter.GetConn("not-a-uuid");
    } catch (e) {
        return true;
    }
    return false;
}

export default function () {
    const ok = check(diameter, {
        "module is loaded": (d) => d !== null && typeof d === "object",
        "Conn constructor is exported": (d) => typeof d.Conn === "function",
        "EnsureConn is exported": (d) => typeof d.EnsureConn === "function",
        "GetConn is exported": (d) => typeof d.GetConn === "function",
        "GetConn error is catchable from JS": () => getConnThrowsOnInvalidId(),
    });
    if (!ok) {
        throw new Error("smoke checks failed");
    }
}
