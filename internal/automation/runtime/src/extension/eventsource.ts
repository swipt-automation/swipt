import {
  boolean,
  brand,
  defineInterface,
  dictionary,
  optional,
  usvString,
} from "../shared/webidl";
import { TextEncoder } from "./encoding";
import { DOMException } from "./errors";
import {
  AbortController,
  Event,
  EventTarget,
  fire,
  MessageEvent,
} from "./events";
import {
  type Connection,
  defaultConnection,
  requestOf,
  sendWith,
} from "./fetch";
import { ReadableStream, TextDecoderStream } from "./streams";
import { URL } from "./url";

const CONNECTING = 0;
const encoder = new TextEncoder();
const OPEN = 1;
const CLOSED = 2;

export const eventSourceInit = dictionary<Web.EventSourceInit>({
  withCredentials: { convert: boolean, fallback: false },
});

let openEventSource: (
  url: string,
  init: Web.EventSourceInit,
  connection: Connection,
) => EventSource;

export const EventSource = defineInterface(
  class EventSource extends EventTarget {
    declare static readonly CONNECTING: 0;
    declare static readonly OPEN: 1;
    declare static readonly CLOSED: 2;
    declare readonly CONNECTING: 0;
    declare readonly OPEN: 1;
    declare readonly CLOSED: 2;
    declare onopen: ((event: globalThis.Event) => unknown) | null;
    declare onmessage: ((event: globalThis.MessageEvent) => unknown) | null;
    declare onerror: ((event: globalThis.Event) => unknown) | null;

    readonly #url: string;
    readonly #withCredentials: boolean;
    readonly #connection: Connection;
    #state: number = CONNECTING;
    #retry = 3000;
    #lastEventId = "";
    #controller = new AbortController();
    #timer: number | undefined;

    static [brand](value: object) {
      return #withCredentials in value;
    }

    static {
      openEventSource = (url, init, connection) =>
        new EventSource(url, init, connection);
    }

    constructor(
      url: string | globalThis.URL,
      init?: Web.EventSourceInit,
      connection: Connection = defaultConnection,
    ) {
      super();
      let parsed: URL;
      try {
        parsed = new URL(url as string);
      } catch {
        throw new DOMException(
          `${JSON.stringify(url)} is not an absolute URL`,
          "SyntaxError",
        );
      }
      if (parsed.username !== "" || parsed.password !== "") {
        throw new DOMException(
          "an EventSource URL can't include credentials",
          "SyntaxError",
        );
      }
      this.#url = parsed.href;
      this.#withCredentials = init!.withCredentials!;
      this.#connection = connection;
      void this.#connect();
    }

    get url(): string {
      return this.#url;
    }

    get withCredentials(): boolean {
      return this.#withCredentials;
    }

    get readyState(): number {
      return this.#state;
    }

    close(): void {
      this.#state = CLOSED;
      clearTimeout(this.#timer);
      this.#controller.abort();
    }

    #reconnect(): void {
      if (this.#state === CLOSED) {
        return;
      }
      this.#state = CONNECTING;
      fire(this, new Event("error"));
      if (this.#state !== CLOSED) {
        this.#timer = setTimeout(() => void this.#connect(), this.#retry);
      }
    }

    #fail(): void {
      if (this.#state === CLOSED) {
        return;
      }
      this.#state = CLOSED;
      fire(this, new Event("error"));
    }

    async #connect(): Promise<void> {
      const controller = new AbortController();
      this.#controller = controller;
      const headers: [string, string][] = [["accept", "text/event-stream"]];
      if (this.#lastEventId !== "") {
        headers.push([
          "last-event-id",
          String.fromCharCode(...encoder.encode(this.#lastEventId)),
        ]);
      }
      let response: Response;
      try {
        response = await sendWith(
          requestOf(
            this.#url,
            {
              headers,
              cache: "no-store",
              credentials: this.#withCredentials ? "include" : "same-origin",
              signal: controller.signal,
            },
            "EventSource",
          ),
          this.#connection,
        );
      } catch {
        if (!controller.signal.aborted) {
          this.#reconnect();
        }
        return;
      }
      if (controller.signal.aborted) {
        return;
      }
      const type = (response.headers.get("content-type") ?? "")
        .split(";")[0]
        .trim()
        .toLowerCase();
      if (
        response.status !== 200 ||
        type !== "text/event-stream" ||
        response.body === null
      ) {
        await response.body?.cancel().catch(() => {});
        this.#fail();
        return;
      }
      const { origin } = new URL(response.url);
      this.#state = OPEN;
      fire(this, new Event("open"));
      let eventType = "";
      let data = "";
      let lastEventId = this.#lastEventId;
      let pending = "";
      let carriage = false;
      const line = (text: string) => {
        if (text === "") {
          this.#lastEventId = lastEventId;
          if (data === "") {
            eventType = "";
            return;
          }
          const event = new MessageEvent(eventType || "message", {
            data: data.endsWith("\n") ? data.slice(0, -1) : data,
            origin,
            lastEventId,
          });
          eventType = "";
          data = "";
          if (this.#state !== CLOSED) {
            fire(this, event);
          }
          return;
        }
        if (text.startsWith(":")) {
          return;
        }
        const colon = text.indexOf(":");
        const field = colon < 0 ? text : text.slice(0, colon);
        let value = colon < 0 ? "" : text.slice(colon + 1);
        if (value.startsWith(" ")) {
          value = value.slice(1);
        }
        switch (field) {
          case "event":
            eventType = value;
            break;
          case "data":
            data += `${value}\n`;
            break;
          case "id":
            if (!value.includes("\0")) {
              lastEventId = value;
            }
            break;
          case "retry":
            if (/^[0-9]+$/.test(value)) {
              this.#retry = Number(value);
            }
            break;
        }
      };
      const reader = (response.body as unknown as ReadableStream<Uint8Array>)
        .pipeThrough(new TextDecoderStream())
        .getReader();
      const read = async () => {
        for (;;) {
          const { done, value } = await reader.read();
          if (done || this.#state === CLOSED) {
            return;
          }
          let text = value;
          if (carriage && text.startsWith("\n")) {
            text = text.slice(1);
          }
          carriage = text.endsWith("\r");
          const parts = (pending + text).split(/\r\n|\r|\n/);
          pending = parts.pop()!;
          for (const part of parts) {
            line(part);
            if (this.#state === CLOSED) {
              return;
            }
          }
        }
      };
      await read().catch(() => {});
      void reader.cancel().catch(() => {});
      if (!controller.signal.aborted) {
        this.#reconnect();
      }
    }
  },
  {
    construct: [usvString, optional(eventSourceInit)],
    operations: { close: [] },
    constants: { CONNECTING, OPEN, CLOSED },
    events: ["open", "message", "error"],
  },
);
export type EventSource = InstanceType<typeof EventSource>;

export const open = (
  url: string,
  init: Web.EventSourceInit,
  connection: Connection,
): EventSource => openEventSource(url, init, connection);
