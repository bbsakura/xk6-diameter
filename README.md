# xk6-diameter

[k6](https://k6.io) extension that speaks the [Diameter Base
Protocol](https://datatracker.ietf.org/doc/html/rfc6733) so you can load
test 3GPP HSS / MME / PCRF and any other Diameter peer directly from a
k6 script. Ships with S6a AIR/ULR helpers and a generic
`sendRequest` / `subscribe` / `serve` API for arbitrary applications.

## Install

Build a custom `k6` binary that includes this extension:

```shell
xk6 build --with github.com/bbsakura/xk6-diameter@latest
```

Requires [xk6](https://github.com/grafana/xk6). See
[k6's extension guide](https://grafana.com/docs/k6/latest/extensions/build-k6-binary-using-go/)
for platform notes.

## Usage

```javascript
import { check } from "k6";
import diameter from "k6/x/diameter";

const conn = diameter.EnsureConn("hss", {
  addr: "127.0.0.1:3868",
  host: "mme.example",
  realm: "example.org",
  network_type: "tcp",
  vendor_id: 10415,
  product_name: "xk6-diameter",
  hostipaddresses: ["127.0.0.1"],
});

export default function () {
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
  check(rc, { "AIA success": (rc) => rc === 2001 });
}
```

Run against a live Diameter peer:

```shell
./k6 run examples/hello-air.js
```

TypeScript declarations are shipped as [`docs/index.d.ts`](docs/index.d.ts).

## API surface

Top-level exports of `k6/x/diameter`:

| Symbol | Purpose |
| --- | --- |
| `EnsureConn(name, options)` | Dial once per name, share the resulting `Conn` across VUs. |
| `new Conn(options)` | Dial without pooling. `.id` is a UUID v7 you can hand to `GetConn`. |
| `GetConn(id)` | Look up a previously created `Conn` by its `id`. |

`Conn` methods:

| Method | Returns | Notes |
| --- | --- | --- |
| `sendAIR(opts)` / `sendULR(opts)` | `Promise<AIA/ULA>` | Async S6a helpers with correlation. |
| `sendRequest(req)` | `Promise<Message>` | Arbitrary Command-Code / Application-Id. |
| `checkSendAIR(opts)` / `checkSendULR(opts)` | `number` (Result-Code) | Blocking form; emits `diameter_tx_duration`. |
| `receive(matcher)` | `Promise<Message>` | One-shot subscriber. |
| `subscribe(matcher)` | `Subscription` | Streaming (chan-like) subscriber. |
| `serve(matcher, cb)` | `ServeHandle` | Streaming subscriber, callback style. |

Full walkthrough for `receive` / `subscribe` / `serve` and the Matcher
DSL: [docs/receiving-messages.md](docs/receiving-messages.md) (Japanese).

## Examples

See the [`examples/`](examples/) directory:

- [`hello-air.js`](examples/hello-air.js) — send one AIR, assert `Result-Code == 2001`.
- [`receive-clr.js`](examples/receive-clr.js) — send AIR while consuming server-initiated CLRs via `subscribe`.
- [`air-ulr-stress.js`](examples/air-ulr-stress.js) — AIR + ULR stress loop with `check()` assertions.

A minimal Diameter server for local development is included at
[`cmd/hss-server`](cmd/hss-server/) (build tag `examples`; `make build`
handles it).

## k6 compatibility

Built and tested against **k6 v1.8.1** (see `go.mod`). The registry
guidelines require compatibility with the latest k6 release or one no
more than three releases old.

## Development

```shell
make install-dev-pkg     # mise-managed toolchain
make install-go-tools    # xk6, gosec, govulncheck, golangci-lint, ...
make build               # produces ./out/bin/k6 with the extension
make test                # go test + integration examples (needs hss-server)
xk6 lint                 # registry compliance check
```

Pre-commit hooks: `pre-commit install`.

## License

[MIT](LICENSE).
