# Receiving messages: `receive` / `subscribe` / `serve`

`Conn` は client-initiated Request/Answer に加えて、**HBH-ID で相関しなかった Answer** と **サーバから来る Request** (CLR 等) を JS 側に受け渡す 3 つの API を提供する。すべて `Matcher` で事前フィルタする。

## 優先順位

1 通のメッセージは **1 つの subscriber にだけ** 配信される。 fan-out
はしない。 N 個の VU が同じ Conn / 同じ matcher で購読を張れば、自然に
ワーカプールになる。

1. **HBH-ID 相関** — `sendRequest` / `checkSendAIR` などが登録した pending Send に該当する Answer は自動でそちらへ (Answer のみ、Request は 2/3 に降りる)。
2. **`receive`** — マッチした最も古い one-shot 待ちに配信し登録解除。 該当があれば 3 へ降りない。
3. **`subscribe`** / **`serve`** — 登録順に走査し、マッチしてかつバッファに空きがある最初の subscription に配信。 満杯の subscription はスキップして次の候補を試すので、遅い consumer が peer を止めない。 該当ゼロなら drop。

## Matcher

```ts
type Matcher = {
  app_id?: number;                   // Diameter Application-Id
  cmd_code?: number | string;        // Command Code (数値) or 名前 ("AIR" / "Authentication-Information" 等)
  is_request?: boolean;              // R-flag。true=Request, false=Answer
  avps?: Array<{ key: string; value: any }>;
};
```

### `cmd_code` の文字列指定

`dict.Default` を引いて名前を code に解決する:

- **long name** — 例 `"Authentication-Information"` / `"Cancel-Location"` (dict の `name` 属性)
- **short name** — 例 `"AI"` / `"CL"` (dict の `short` 属性)
- **short + R/A サフィックス** — 例 `"AIR"` / `"AIA"` / `"CLR"` / `"CLA"` 。 末尾の `R`/`A` が strip され short と照合される。 Request/Answer の区別は `is_request` で行う (`AIR` と `AIA` は同一 code)。

未知の名前を渡すと matcher は何にも match しない (未知 AVP key と同じ挙動)。カスタム application dict を使う場合は `dict.LoadFile` などで dict.Default に事前ロードするか、数値 code を直接指定する。

- 各フィールドは省略時 wildcard。全省略の `{}` はすべてのメッセージにマッチ。
- 複数指定は **AND**。`avps` の各要素も全て一致する必要がある。
- `avps[].key` は message の dictionary で解決する AVP 名 (例 `"User-Name"`, `"Session-Id"`, `"Visited-PLMN-Id"`)。value は Send 側と同じ型変換ルール (`string` / `[]byte` / JS int-array for OctetString、`number` for numeric 型など)。
- Grouped AVP の内部フィールドは絞れない (top-level のみ)。
- 未知の AVP 名、AVP 欠落、値不一致はすべて non-match。

例:
```js
// CLR (Cancel Location Request) — 文字列でも数値でも
{ cmd_code: "CLR" }                                 // "CL" short + "R" サフィックスで解決
{ cmd_code: "Cancel-Location", is_request: true }   // long name
{ cmd_code: 317, is_request: true }                 // 数値 code 直指定

// 特定 Session + User の CLR
{
  cmd_code: "CLR",
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
  const req = await conn.receive({ cmd_code: "CLR", is_request: true });
  console.log("got CLR from", req.Header.OriginHost);
  // 現状 API は受信側の Answer 送信を提供していない。将来 `SendAnswer`
  // 相当を追加予定。 (echo する HBH-ID / E2E-ID を保持したまま Answer を
  // 組み立てる必要があり、`sendRequest` は新 HBH-ID を割り当てるため代用
  // できない。)
}
```

タイムアウトを付けたい場合は `Promise.race` は使えない — 敗者側の
`receive()` は登録が残ったままになり、後続メッセージを黙って食う。
代わりに `subscribe` + close で表現する:

```js
const sub = conn.subscribe({ cmd_code: "CLR", is_request: true });
const timer = setTimeout(() => sub.close(), 5000);
try {
  const msg = await sub.recv();
  clearTimeout(timer);
  // ... 処理 ...
} catch (e) {
  // タイムアウトで close された場合 recv() が reject
} finally {
  sub.close();
}
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
- 内部バッファ 256 通。 このバッファが満杯だと dispatcher は次にマッチする subscription にスキップする。 全 subscription が満杯なら drop。

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

## 完全な例: CLR を非同期に処理する

配信は先着 1 subscription なので、N VU が同じ Conn に対して subscribe/serve
を張れば自然にワーカプールになる:

```js
import diameter from "k6/x/diameter";

const connOpts = {
  addr: "127.0.0.1:3868",
  host: "mme.test",
  realm: "test.realm",
  network_type: "tcp",
  vendor_id: 10415,
  product_name: "xk6-diameter",
  hostipaddresses: ["127.0.0.1"],
};

const conn = diameter.EnsureConn("hss", connOpts);

export default async function () {
  const sub = conn.subscribe({ cmd_code: "CLR", is_request: true });
  const clrLoop = (async () => {
    try {
      for (;;) {
        const clr = await sub.recv();
        console.log("CLR:", clr.Header.HopByHopID);
      }
    } catch (_) { /* subscription closed / iteration ended */ }
  })();

  // 通常の AIR/ULR ワークロード
  await conn.sendAIR({ /* ... */ });

  sub.close();
  await clrLoop;
}
```

1 通の CLR に対する簡潔版として、iteration 内で `receive` を直接使う
パターンも同じセマンティクス (先着 1 VU が取る):

```js
export default async function () {
  const clr = await conn.receive({ cmd_code: "CLR", is_request: true });
  console.log("CLR:", clr.Header.HopByHopID);
}
```

## 注意点

- **共有 Conn では subscription も共有される**。`EnsureConn` で複数 VU が同じ `Conn` を掴んでいる場合、あるメッセージは登録順で先着 1 subscription にしか届かない。特定 VU に配りたい場合は `avps` で `Session-Id` / `User-Name` を絞る。
- **iteration 終了時に pending な `receive` / `sub.recv()` は reject** される。`await` を残したまま iteration が抜けないよう注意。
- **全 subscription が満杯なら drop**。厳密な取り逃しゼロが必要なら consumer 側で `recv` を高頻度で回すか、subscription を増やす。
