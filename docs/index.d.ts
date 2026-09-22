/**
 * xk6-diameter — a k6 extension that speaks the Diameter Base Protocol
 * (RFC 6733) with S6a helpers for AIR/ULR.
 *
 * Import in a k6 script as:
 *   import diameter from "k6/x/diameter";
 */
declare module "k6/x/diameter" {
  /** Options accepted by {@link Diameter.EnsureConn}, `new Conn()` and Send* helpers. */
  export interface ConnectionOptions {
    /** `ip:port` of the Diameter peer (required for dial). */
    addr?: string;
    /** Origin-Host to advertise (RFC 6733 §6.3). */
    host?: string;
    /** Origin-Realm to advertise. */
    realm?: string;
    /** `tcp` (default) or `sctp`. */
    network_type?: "tcp" | "sctp";
    /** Max transport-level retransmits. */
    retries?: number;
    /** Enterprise Vendor-Id (e.g. `10415` for 3GPP). */
    vendor_id?: number;
    /** Product-Name AVP advertised in CER. */
    product_name?: string;
    /** Host-IP-Address AVPs advertised in CER. */
    hostipaddresses?: string[];
    /** Auth-Application-Id used to negotiate the app. */
    app_id?: number;
    /** IMSI used by the built-in AIR/ULR helpers. */
    ueimsi?: string;
    /** Visited-PLMN-Id (raw bytes, hex string) used by AIR/ULR helpers. */
    plmn_id?: string;
    /** Requested-EUTRAN-Authentication-Info: Number-Of-Requested-Vectors. */
    vectors?: number;
    /**
     * How long (seconds) a CheckSend* / Send* Promise waits for the peer's
     * Answer before rejecting. `0` returns immediately.
     */
    completion_sleep?: number;
    /** Override Session-Id (generated if omitted). */
    session_id?: string;
    /** Destination-Host AVP; omit to let the peer's Origin-Host be used. */
    destination_host?: string;
    /** Destination-Realm AVP. */
    destination_realm?: string;
    /** OR the P-bit into the Command Flags. */
    proxiable_flag?: boolean;
    /** Extra top-level AVPs appended to the request. */
    additional?: AVP[];
  }

  /**
   * A (key, value) pair resolved to a Diameter AVP at send time via the
   * peer's dictionary. `value` can be a scalar (number/string/bytes) or an
   * array of nested {@link AVP} for a Grouped AVP.
   */
  export interface AVP {
    key: string;
    value: string | number | number[] | AVP[];
  }

  /** An arbitrary Diameter command for {@link Conn.sendRequest}. */
  export interface Request {
    /** Application-Id (e.g. `16777251` for S6a). */
    app_id: number;
    /** Command-Code. */
    cmd: number;
    /** Extra Command-Flags OR'd on top of the Request bit. */
    flags?: number;
    /** Full AVP set — nothing is auto-injected. */
    avps: AVP[];
    /** Same semantics as {@link ConnectionOptions.completion_sleep}. */
    completion_sleep?: number;
  }

  /**
   * Filter for incoming messages. Missing fields are wildcards; only the
   * present fields participate in the AND check. See
   * `docs/receiving-messages.md`.
   */
  export interface Matcher {
    app_id?: number;
    /** Numeric command code or command short name (e.g. `"CLR"`). */
    cmd_code?: number | string;
    is_request?: boolean;
    avps?: AVP[];
  }

  /** A decoded Diameter message handed to Receive/Subscribe/Serve consumers. */
  export interface Message {
    [key: string]: unknown;
  }

  /** S6a Authentication-Information-Answer (subset). */
  export interface AIA {
    SessionID: string;
    ResultCode: number;
    OriginHost: string;
    OriginRealm: string;
    [key: string]: unknown;
  }

  /** S6a Update-Location-Answer (subset). */
  export interface ULA {
    SessionID: string;
    ULAFlags: number;
    ResultCode: number;
    OriginHost: string;
    OriginRealm: string;
    [key: string]: unknown;
  }

  /** Handle for a streaming {@link Conn.subscribe} consumer. */
  export interface Subscription {
    /** Resolves with the next matching message or rejects when closed. */
    recv(): Promise<Message>;
    /** Unregister and cancel any pending `recv()`. Idempotent. */
    close(): void;
  }

  /** Handle for a callback-style {@link Conn.serve} consumer. */
  export interface ServeHandle {
    /** Unregister and stop the callback loop. Idempotent. */
    close(): void;
  }

  /** Shared Diameter connection returned by every constructor. */
  export interface Conn {
    /** UUID v7 identifying the underlying shared *Client. */
    readonly id: string;

    /** Fire an S6a AIR and resolve with the decoded AIA. */
    sendAIR(options: ConnectionOptions): Promise<AIA>;
    /** Fire an S6a ULR and resolve with the decoded ULA. */
    sendULR(options: ConnectionOptions): Promise<ULA>;
    /** Fire an arbitrary Request and resolve with the raw Answer. */
    sendRequest(req: Request): Promise<Message>;

    /** Blocking AIR that also emits `diameter_tx_duration`. Returns Result-Code. */
    checkSendAIR(options: ConnectionOptions): number;
    /** Blocking ULR that also emits `diameter_tx_duration`. Returns Result-Code. */
    checkSendULR(options: ConnectionOptions): number;

    /** One-shot: resolve with the first matching message. */
    receive(matcher: Matcher): Promise<Message>;
    /** Streaming (chan-like) subscriber; close when done. */
    subscribe(matcher: Matcher): Subscription;
    /** Streaming subscriber whose messages are pushed to `cb`. */
    serve(matcher: Matcher, cb: (msg: Message) => void): ServeHandle;
  }

  /** Constructor for a new Diameter connection. */
  export interface ConnConstructor {
    new (options: ConnectionOptions): Conn;
  }

  /**
   * Default export from `k6/x/diameter`. All Conn constructors return the
   * same shared *Client under the hood; multiple VUs reusing the same
   * `EnsureConn` name share one dialed connection.
   */
  export interface Diameter {
    /** Direct constructor form: `new diameter.Conn(options)`. */
    Conn: ConnConstructor;
    /** Named pool: dial once per name, then reuse across VUs. */
    EnsureConn(name: string, options: ConnectionOptions): Conn;
    /** Look up a previously created Conn by its `.id`. */
    GetConn(id: string): Conn;
  }

  const diameter: Diameter;
  export default diameter;
}
