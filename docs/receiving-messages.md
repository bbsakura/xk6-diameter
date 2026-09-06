# Receiving messages: `receive` / `subscribe` / `serve`

`Conn` は client-initiated Request/Answer に加えて、**HBH-ID で相関しなかった Answer** と **サーバから来る Request** (CLR 等) を JS 側に受け渡す 3 つの API を提供する。すべて `Matcher` で事前フィルタする。

## 優先順位

1 通のメッセージが到着すると、次の順で最初にマッチしたパスにだけ届く。

1. **HBH-ID 相関** — `sendRequest` / `checkSendAIR` などが登録した pending Send に該当する Answer は自動でそちらへ。
2. **`receive`** — マッチした最も古い one-shot 待ちに 1 通配信して登録解除。
3. **`serve`** / **`subscribe`** — マッチした全 subscription に fan-out (channel が満杯なら drop)。

## Matcher

```ts
type Matcher = {
  app_id?: number;           // Diameter Application-Id
  cmd_code?: number;         // Command Code
  is_request?: boolean;      // R-flag。true=Request, false=Answer
  avps?: Array<{ key: string; value: any }>;
};
```

- 各フィールドは省略時 wildcard。全省略の `{}` はすべてのメッセージにマッチ。
- 複数指定は **AND**。`avps` の各要素も全て一致する必要がある。
- `avps[].key` は message の dictionary で解決する AVP 名 (例 `"User-Name"`, `"Session-Id"`, `"Visited-PLMN-Id"`)。value は Send 側と同じ型変換ルール (`string` / `[]byte` / JS int-array for OctetString、`number` for numeric 型など)。
- Grouped AVP の内部フィールドは絞れない (top-level のみ)。
- 未知の AVP 名、AVP 欠落、値不一致はすべて non-match。

例:
```js
// CLR (Cancel Location Request)
{ cmd_code: 316, is_request: true }

// 特定 Session + User の CLR
{
  cmd_code: 316,
  is_request: true,
  avps: [
    { key: "Session-Id", value: "sess;42" },
    { key: "User-Name",  value: "001010000000001" },
  ],
}

// S6a application の全 Request
{ app_id: 16777251, is_request: true }
```

## `conn.receive(matcher) : Promise<Message>`

1 通だけ待つ。次にマッチしたメッセージで resolve、iteration 終了で reject。

```js
import diameter from "k6/x/diameter";

const conn = diameter.EnsureConn("hss", { /* connect opts */ });

export default async function () {
  const req = await conn.receive({ cmd_code: 316, is_request: true });
  console.log("got CLR from", req.Header.OriginHost);
  // Answer を返したい場合は自前で組み立てて conn.sendRequest(...) する
}
```

タイムアウトが要る場合は `Promise.race` で:

```js
const withTimeout = (p, ms) =>
  Promise.race([p, new Promise((_, rj) => setTimeout(() => rj("timeout"), ms))]);

const msg = await withTimeout(conn.receive({}), 5000);
```

## `conn.subscribe(matcher) : Subscription` (chan-like)

継続受信を Go の channel 風に消費する。`sub.recv()` で 1 通ずつ Promise を取り、`sub.close()` で購読解除。

```ts
interface Subscription {
  recv(): Promise<Message>;
  close(): void;
}
```

```js
const sub = conn.subscribe({ app_id: 16777251, is_request: true });
try {
  for (;;) {
    const msg = await sub.recv();
    // 処理
  }
} finally {
  sub.close();
}
```

- `sub.recv()` は close 済み、または iteration 終了時に reject。
- 内部バッファ 256 通。消費が追いつかない場合、新規到着は drop される (blocking しない)。

## `conn.serve(matcher, cb) : ServeHandle` (callback)

到着ごとにコールバックが呼ばれる。イベントリスナ的な使い方。

```ts
interface ServeHandle {
  close(): void;
}
```

```js
const handle = conn.serve({ app_id: 16777251 }, (msg) => {
  console.log(msg.Header.CommandCode);
});

// ... 適当な時点で:
handle.close();
```

- `cb` は必須。省略すると例外。
- 内部で goroutine が回り、k6 event loop 経由で cb を呼ぶ。iteration 内で処理される。
- 内部バッファは `subscribe` と同じ。

## 使い分けの目安

| ケース | 推奨 API |
|---|---|
| 1 通だけ待つ (テスト assertion, handshake) | `receive` |
| ループで順次処理したい | `subscribe` |
| イベントリスナ的に登録して寝かせたい | `serve` |
| 送信 Request の応答 | `sendRequest` (このドキュメントは対象外) |

## 完全な例: CLR にログして無視する

```js
import diameter from "k6/x/diameter";

const conn = diameter.EnsureConn("hss", {
  addr: "127.0.0.1:3868",
  host: "mme.test",
  realm: "test.realm",
  network_type: "tcp",
  vendor_id: 10415,
  product_name: "xk6-diameter",
  hostipaddresses: ["127.0.0.1"],
});

// VU 起動時に一度だけ CLR ハンドラを登録 (共有 conn なので 1 つで十分)
const clrHandle = conn.serve(
  { cmd_code: 316, is_request: true },
  (clr) => console.log("CLR:", clr.Header.HopByHopID),
);

export default function () {
  // 通常の AIR/ULR ワークロード
  conn.checkSendAIR({ /* ... */ });
}

export function teardown() {
  clrHandle.close();
}
```

## 注意点

- **共有 Conn では subscription も共有される**。`EnsureConn` で複数 VU が同じ `Conn` を掴んでいる場合、ある VU が登録した `receive` は他 VU 宛の Request も食う可能性がある。VU ごとに分けたい場合は `avps` で `Session-Id` / `User-Name` を絞る。
- **iteration 終了時に pending な `receive` / `sub.recv()` は reject** される。`await` を残したまま iteration が抜けないよう注意。
- **buffer 満杯時は drop**。厳密な取り逃しゼロが必要なら consumer 側で `recv` を高頻度で回す。
