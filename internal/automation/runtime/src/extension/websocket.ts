import {
  brand,
  bufferSource,
  defineInterface,
  domString,
  enumeration,
  implementsInterface,
  integer,
  isBufferSource,
  isObject,
  iterableMethod,
  optional,
  sequenceFrom,
  usvString,
  type Converter,
} from "../shared/webidl";
import { invoke } from "./async";
import { Blob, blobBytes } from "./blob";
import { bytesOf, TextEncoder } from "./encoding";
import { DOMException } from "./errors";
import {
  AbortController,
  CloseEvent,
  ErrorEvent,
  Event,
  EventTarget,
  fire,
  MessageEvent,
} from "./events";
import { type Connection, defaultConnection } from "./fetch";
import { native } from "./native";
import { URL } from "./url";

interface Opened {
  socket: string;
  protocol: string;
  extensions: string;
}

interface Closed {
  code: number;
  reason: string;
  clean: boolean;
  error?: string;
}

const CONNECTING = 0;
const OPEN = 1;
const CLOSING = 2;
const CLOSED = 3;

type ReadyState = 0 | 1 | 2 | 3;

const token = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;
const encoder = new TextEncoder();

const protocolSequence: Converter<string[]> = (value, context) => {
  const method = isObject(value) ? iterableMethod(value, context) : undefined;
  return method === undefined
    ? [domString(value, context)]
    : sequenceFrom(value as object, method, domString, context);
};

export const protocolList: Converter<string[]> = (value, context) => {
  if (!isObject(value) || iterableMethod(value, context) !== undefined) {
    return protocolSequence(value, context);
  }
  const { protocols } = value as { protocols?: unknown };
  return protocols === undefined
    ? []
    : protocolSequence(protocols, `${context}'s protocols`);
};

const messageData: Converter<string | Blob | ArrayBuffer | ArrayBufferView> = (
  value,
  context,
) => {
  if (isBufferSource(value)) {
    return bufferSource(value, context);
  }
  return implementsInterface(Blob, value)
    ? (value as Blob)
    : usvString(value, context);
};

const syntaxError = (message: string) =>
  new DOMException(message, "SyntaxError");

let connectWebSocket: (
  url: string,
  protocols: string[],
  connection: Connection,
) => WebSocket;

export const WebSocket = defineInterface(
  class WebSocket extends EventTarget {
    declare static readonly CONNECTING: 0;
    declare static readonly OPEN: 1;
    declare static readonly CLOSING: 2;
    declare static readonly CLOSED: 3;
    declare readonly CONNECTING: 0;
    declare readonly OPEN: 1;
    declare readonly CLOSING: 2;
    declare readonly CLOSED: 3;
    declare onopen: ((event: globalThis.Event) => unknown) | null;
    declare onmessage: ((event: globalThis.MessageEvent) => unknown) | null;
    declare onerror: ((event: globalThis.Event) => unknown) | null;
    declare onclose: ((event: globalThis.CloseEvent) => unknown) | null;

    readonly #url: string;
    readonly #origin: string;
    readonly #connecting = new AbortController();
    #binaryType: "blob" | "arraybuffer" = "blob";
    #state: ReadyState = CONNECTING;
    #protocol = "";
    #extensions = "";
    #buffered = 0;
    #socket = "";
    #sending: Promise<unknown> = Promise.resolve();

    static [brand](value: object) {
      return #binaryType in value;
    }

    static {
      connectWebSocket = (url, protocols, connection) =>
        new WebSocket(url, protocols, connection);
    }

    constructor(
      url: string | globalThis.URL,
      protocols?: string | string[],
      connection: Connection = defaultConnection,
    ) {
      super();
      let parsed: URL;
      try {
        parsed = new URL(url as string);
      } catch {
        throw syntaxError(`${JSON.stringify(url)} is not an absolute URL`);
      }
      if (parsed.protocol === "http:" || parsed.protocol === "https:") {
        parsed.protocol = parsed.protocol === "http:" ? "ws:" : "wss:";
      }
      if (parsed.protocol !== "ws:" && parsed.protocol !== "wss:") {
        throw syntaxError(
          `a WebSocket URL is ws: or wss:, not ${parsed.protocol}`,
        );
      }
      if (parsed.hash !== "" || parsed.href.endsWith("#")) {
        throw syntaxError("a WebSocket URL can't have a fragment");
      }
      const list = protocols as string[];
      if (
        list.some((protocol) => !token.test(protocol)) ||
        new Set(list.map((protocol) => protocol.toLowerCase())).size !==
          list.length
      ) {
        throw syntaxError("the protocols are not unique tokens");
      }
      this.#url = parsed.href;
      this.#origin = parsed.origin;
      void this.#connect(list, connection);
    }

    async #connect(protocols: string[], connection: Connection): Promise<void> {
      let opened: Opened;
      try {
        opened = await invoke<Opened>(
          "websocket.connect",
          [this.#url, protocols, connection.headers, connection.transport],
          { signal: this.#connecting.signal },
        );
      } catch (error) {
        this.#finish(1006, "", false, error);
        return;
      }
      if (this.#state !== CONNECTING) {
        native.release(opened.socket);
        this.#finish(1006, "", false, this.#connecting.signal.reason);
        return;
      }
      this.#socket = opened.socket;
      this.#protocol = opened.protocol;
      this.#extensions = opened.extensions;
      this.#state = OPEN;
      fire(this, new Event("open"));
      void this.#receive();
    }

    async #receive(): Promise<void> {
      for (;;) {
        let value: string | Uint8Array | Closed;
        try {
          value = await invoke<string | Uint8Array | Closed>("websocket.read", [
            this.#socket,
          ]);
        } catch (error) {
          this.#finish(1006, "", false, error);
          return;
        }
        if (typeof value === "string" || value instanceof Uint8Array) {
          if (this.#state !== OPEN) {
            continue;
          }
          const data =
            typeof value === "string"
              ? value
              : this.#binaryType === "arraybuffer"
                ? value.byteOffset === 0 &&
                  value.buffer.byteLength === value.byteLength
                  ? value.buffer
                  : value.slice().buffer
                : new Blob([value]);
          fire(
            this,
            new MessageEvent("message", { data, origin: this.#origin }),
          );
          continue;
        }
        this.#finish(
          value.code,
          value.reason,
          value.clean,
          value.error === undefined ? undefined : new Error(value.error),
        );
        return;
      }
    }

    #finish(
      code: number,
      reason: string,
      clean: boolean,
      error?: unknown,
    ): void {
      if (this.#state === CLOSED) {
        return;
      }
      this.#state = CLOSED;
      this.#socket = "";
      if (!clean) {
        fire(
          this,
          new ErrorEvent("error", {
            message: error instanceof Error ? error.message : "",
            error,
          }),
        );
      }
      fire(this, new CloseEvent("close", { code, reason, wasClean: clean }));
    }

    get url(): string {
      return this.#url;
    }

    get readyState(): ReadyState {
      return this.#state;
    }

    get bufferedAmount(): number {
      return this.#buffered;
    }

    get extensions(): string {
      return this.#extensions;
    }

    get protocol(): string {
      return this.#protocol;
    }

    get binaryType(): "blob" | "arraybuffer" {
      return this.#binaryType;
    }

    set binaryType(value: "blob" | "arraybuffer") {
      this.#binaryType = value;
    }

    send(data: string | Blob | ArrayBuffer | ArrayBufferView): void {
      if (this.#state === CONNECTING) {
        throw new DOMException(
          "the WebSocket is still connecting",
          "InvalidStateError",
        );
      }
      const binary = typeof data !== "string";
      const bytes =
        typeof data === "string"
          ? encoder.encode(data)
          : implementsInterface(Blob, data)
            ? blobBytes(data as Blob)
            : bytesOf(data);
      const size = bytes.length;
      this.#buffered += size;
      if (this.#state !== OPEN) {
        return;
      }
      const args = binary
        ? [this.#socket, native.put(bytes), true]
        : [this.#socket, data, false];
      this.#sending = this.#sending
        .then(() => invoke("websocket.send", args))
        .catch(() => {})
        .finally(() => {
          this.#buffered -= size;
        });
    }

    close(code?: number, reason?: string): void {
      if (code !== undefined && code !== 1000 && (code < 3000 || code > 4999)) {
        throw new DOMException(
          `the close code ${code} is not 1000 or in 3000 to 4999`,
          "InvalidAccessError",
        );
      }
      if (reason !== undefined && encoder.encode(reason).length > 123) {
        throw syntaxError("the close reason is over 123 bytes");
      }
      if (this.#state === CLOSING || this.#state === CLOSED) {
        return;
      }
      if (this.#state === CONNECTING) {
        this.#state = CLOSING;
        this.#connecting.abort(
          new DOMException(
            "the WebSocket closed before it connected",
            "AbortError",
          ),
        );
        return;
      }
      this.#state = CLOSING;
      const socket = this.#socket;
      this.#sending = this.#sending
        .then(() =>
          invoke("websocket.close", [socket, code ?? 1005, reason ?? ""]),
        )
        .catch(() => {});
    }
  },
  {
    construct: [usvString, optional(protocolList, [])],
    operations: {
      send: [messageData],
      close: [
        optional(integer("unsigned short", "Clamp")),
        optional(usvString),
      ],
    },
    setters: { binaryType: enumeration(["blob", "arraybuffer"]) },
    constants: { CONNECTING, OPEN, CLOSING, CLOSED },
    events: ["open", "message", "error", "close"],
  },
);
export type WebSocket = InstanceType<typeof WebSocket>;

export const connect = (
  url: string,
  protocols: string[],
  connection: Connection,
): WebSocket => connectWebSocket(url, protocols, connection);
