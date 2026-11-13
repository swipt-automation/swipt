import dns from "node:dns";
import { createRequire } from "node:module";

import {
  type DescMessage,
  type DescMethod,
  type DescService,
  type Message,
  type MessageInitShape,
  type MessageShape,
  create,
  fromBinary,
  toBinary,
} from "@bufbuild/protobuf";
import type { GenService } from "@bufbuild/protobuf/codegenv2";

import { resolveHost } from "./mdns";
import { LogService } from "./proto/logs_pb";
import { REPLService } from "./proto/repl_pb";
import { SwipletService } from "./proto/swiplets_pb";
import { SystemService } from "./proto/system_pb";

const largestMessage = 128 << 20;
const resolveLimit = 5000;
const contentType = "application/grpc-web+proto";
const webSocketSubprotocol = "grpc-websockets";
const { apiLevel } = createRequire(import.meta.url)("@swipt/api/package.json");

dns.setDefaultResultOrder("ipv4first");

export const Code = {
  Canceled: 1,
  Unknown: 2,
  DeadlineExceeded: 4,
  NotFound: 5,
  Unimplemented: 12,
  Internal: 13,
  Unavailable: 14,
  Unauthenticated: 16,
};

export class UsageError extends Error {}

export class RpcError extends Error {
  declare code: number;

  constructor(code: number, message?: string) {
    super(message);
    this.code = code;
  }
}

const httpStatusCodes: Record<number, number> = {
  400: Code.Internal,
  401: Code.Unauthenticated,
  403: Code.Unauthenticated,
  404: Code.Unimplemented,
  429: Code.Unavailable,
  502: Code.Unavailable,
  503: Code.Unavailable,
  504: Code.Unavailable,
};

function statusMessage(text: string | undefined) {
  if (text === undefined) {
    return "";
  }
  try {
    return decodeURIComponent(text);
  } catch {
    return text;
  }
}

interface Cause {
  errors?: Cause[];
  message?: string;
  code?: string;
}

const causeText = (cause?: Cause): string | undefined =>
  cause?.errors?.length
    ? cause.errors.map(causeText).join("; ")
    : cause?.message || cause?.code;

const transportError = (error: unknown) => {
  if (error instanceof RpcError) {
    return error;
  }
  if (!(error instanceof Error)) {
    return new RpcError(Code.Unavailable, String(error));
  }
  if (error.name === "AbortError") {
    return new RpcError(Code.Canceled, "the call was canceled");
  }
  return new RpcError(
    Code.Unavailable,
    causeText(error.cause as Cause) || error.message || String(error),
  );
};

interface Connection {
  url: string;
  token: string;
  resolved?: string;
}

export interface CallOptions {
  signal?: AbortSignal;
}

async function locate(connection: Connection, signal?: AbortSignal) {
  const url = new URL(connection.url);
  if (!url.hostname.endsWith(".local")) {
    return connection.url;
  }
  let address;
  try {
    address = await resolveHost(
      url.hostname,
      AbortSignal.any([
        ...(signal ? [signal] : []),
        AbortSignal.timeout(resolveLimit),
      ]),
    );
  } catch (error) {
    throw transportError(error);
  }
  if (signal?.aborted) {
    throw new RpcError(Code.Canceled, "the call was canceled");
  }
  if (address === undefined) {
    throw new RpcError(
      Code.Unavailable,
      `${url.hostname} isn't found on the local network`,
    );
  }
  url.hostname = address.includes(":") ? `[${address}]` : address;
  return url.href.replace(/\/+$/, "");
}

async function base(connection: Connection, signal?: AbortSignal) {
  connection.resolved ??= await locate(connection, signal);
  return connection.resolved;
}

function frame(schema: DescMessage, init: MessageInitShape<DescMessage>) {
  const body = toBinary(schema, create(schema, init));
  const out = Buffer.allocUnsafe(5 + body.length);
  out[0] = 0;
  out.writeUInt32BE(body.length, 1);
  out.set(body, 5);
  return out;
}

function parseMetadata(body: Uint8Array) {
  const headers: Record<string, string> = {};
  for (const line of Buffer.from(body).toString().split("\r\n")) {
    const separator = line.indexOf(":");
    if (separator > 0) {
      headers[line.slice(0, separator).trim().toLowerCase()] = line
        .slice(separator + 1)
        .trim();
    }
  }
  return headers;
}

type Frame = { trailers: Record<string, string> } | { message: Message };

class FrameReader {
  declare schema: DescMessage;
  pending: Buffer = Buffer.alloc(0);

  constructor(schema: DescMessage) {
    this.schema = schema;
  }

  push(chunk: Uint8Array) {
    this.pending =
      this.pending.length === 0
        ? Buffer.from(chunk)
        : Buffer.concat([this.pending, Buffer.from(chunk)]);
    const frames: Frame[] = [];
    while (this.pending.length >= 5) {
      const length = this.pending.readUInt32BE(1);
      if (length > largestMessage) {
        throw new RpcError(
          Code.Internal,
          `the server sent a message of ${length} bytes`,
        );
      }
      if (this.pending.length < 5 + length) {
        break;
      }
      const body = this.pending.subarray(5, 5 + length);
      frames.push(
        (this.pending[0] & 0x80) !== 0
          ? { trailers: parseMetadata(body) }
          : { message: fromBinary(this.schema, body) },
      );
      this.pending = this.pending.subarray(5 + length);
    }
    return frames;
  }
}

function checkStatus(headers: Record<string, string>) {
  const status = headers["grpc-status"];
  if (status === undefined) {
    return false;
  }
  const code = Number(status);
  if (code !== 0) {
    throw new RpcError(code, statusMessage(headers["grpc-message"]));
  }
  return true;
}

function requestHeaders(connection: Connection) {
  const headers: Record<string, string> = {
    "content-type": contentType,
    "x-grpc-web": "1",
    "swipt-api-level": String(apiLevel),
  };
  if (connection.token !== "") {
    headers.authorization = `Bearer ${connection.token}`;
  }
  return headers;
}

async function* invokeHTTP(
  connection: Connection,
  method: DescMethod,
  input: MessageInitShape<DescMessage>,
  { signal }: CallOptions,
) {
  let response;
  try {
    response = await fetch(
      `${await base(connection, signal)}/${method.parent.typeName}/${method.name}`,
      {
        method: "POST",
        headers: requestHeaders(connection),
        body: frame(method.input, input),
        signal,
      },
    );
  } catch (error) {
    throw transportError(error);
  }
  const headers = Object.fromEntries(response.headers);
  if (response.status !== 200) {
    await response.body?.cancel();
    throw new RpcError(
      httpStatusCodes[response.status] ?? Code.Unknown,
      `HTTP ${response.status}`,
    );
  }
  if (checkStatus(headers)) {
    await response.body?.cancel();
    return;
  }
  const reader = new FrameReader(method.output);
  let ended = false;
  try {
    for await (const chunk of response.body ?? []) {
      for (const found of reader.push(chunk)) {
        if ("trailers" in found) {
          checkStatus(found.trailers);
          ended = true;
        } else if (!ended) {
          yield found.message;
        }
      }
    }
  } catch (error) {
    throw transportError(error);
  }
  if (!ended) {
    throw new RpcError(Code.Unavailable, "the response ended without a status");
  }
}

async function* invokeWebSocket(
  connection: Connection,
  method: DescMethod,
  input: AsyncIterable<MessageInitShape<DescMessage>>,
  { signal }: CallOptions,
) {
  if (signal?.aborted) {
    throw new RpcError(Code.Canceled, "the call was canceled");
  }
  const webSocketBase = (await base(connection, signal)).replace(/^http/, "ws");
  const socket = new WebSocket(
    `${webSocketBase}/${method.parent.typeName}/${method.name}`,
    [webSocketSubprotocol],
  );
  socket.binaryType = "arraybuffer";
  const reader = new FrameReader(method.output);
  const messages: Message[] = [];
  let failure: unknown;
  let closed = false;
  let headersSeen = false;
  let wake: (value?: unknown) => void = () => {};
  const notify = () => {
    const current = wake;
    wake = () => {};
    current();
  };
  const fail = (error: unknown) => {
    failure ??= error;
    socket.close();
    notify();
  };
  const abort = () =>
    fail(new RpcError(Code.Canceled, "the call was canceled"));
  const opened = new Promise((resolve, reject) => {
    socket.addEventListener("open", resolve, { once: true });
    socket.addEventListener(
      "close",
      () =>
        reject(
          failure ??
            new RpcError(
              Code.Unavailable,
              `cannot connect to ${webSocketBase}`,
            ),
        ),
      { once: true },
    );
  });
  socket.addEventListener("message", (event) => {
    try {
      for (const found of reader.push(new Uint8Array(event.data))) {
        if ("message" in found) {
          messages.push(found.message);
        } else if (headersSeen || "grpc-status" in found.trailers) {
          checkStatus(found.trailers);
          closed = true;
          socket.close();
        } else {
          headersSeen = true;
        }
      }
    } catch (error) {
      fail(error);
    }
    notify();
  });
  socket.addEventListener("close", (event) => {
    if (!closed) {
      failure ??= new RpcError(
        Code.Unavailable,
        event.reason || "the connection closed before the response",
      );
    }
    closed = true;
    notify();
  });
  socket.addEventListener("error", () => {
    failure ??= new RpcError(
      Code.Unavailable,
      `cannot connect to ${webSocketBase}`,
    );
  });
  signal?.addEventListener("abort", abort, { once: true });
  try {
    await opened;
    const headers = requestHeaders(connection);
    socket.send(
      Buffer.from(
        `${Object.entries(headers)
          .map(([key, value]) => `${key}: ${value}\r\n`)
          .join("")}`,
      ),
    );
    void (async () => {
      try {
        for await (const message of input) {
          if (closed || failure) {
            return;
          }
          socket.send(
            Buffer.concat([Buffer.from([0]), frame(method.input, message)]),
          );
        }
        if (!closed && !failure) {
          socket.send(Buffer.from([1]));
        }
      } catch (error) {
        fail(transportError(error));
      }
    })();
    while (true) {
      const message = messages.shift();
      if (message !== undefined) {
        yield message;
        continue;
      }
      if (closed || failure) {
        break;
      }
      await new Promise((resolve) => {
        wake = resolve;
      });
    }
    if (failure) {
      throw failure;
    }
  } finally {
    signal?.removeEventListener("abort", abort);
    if (socket.readyState !== WebSocket.CLOSED) {
      socket.close();
    }
  }
}

async function single(iterable: AsyncIterable<Message>) {
  let found: Message | undefined;
  for await (const message of iterable) {
    found ??= message;
  }
  if (found === undefined) {
    throw new RpcError(Code.Internal, "the server sent no response");
  }
  return found;
}

type Call<Method extends Pick<DescMethod, "methodKind" | "input" | "output">> =
  Method["methodKind"] extends "unary"
    ? (
        input: MessageInitShape<Method["input"]>,
        options?: CallOptions,
      ) => Promise<MessageShape<Method["output"]>>
    : Method["methodKind"] extends "server_streaming"
      ? (
          input: MessageInitShape<Method["input"]>,
          options?: CallOptions,
        ) => AsyncGenerator<MessageShape<Method["output"]>, void>
      : (
          input: AsyncIterable<MessageInitShape<Method["input"]>>,
          options?: CallOptions,
        ) => Promise<MessageShape<Method["output"]>>;

type ServiceClient<Service> =
  Service extends GenService<infer Methods>
    ? { [Name in keyof Methods]: Call<Methods[Name]> }
    : never;

function bind<Service extends DescService>(
  service: Service,
  connection: Connection,
) {
  return Object.fromEntries(
    service.methods.map((method) => [
      method.localName,
      (input: never, options: CallOptions = {}) => {
        switch (method.methodKind) {
          case "unary":
            return single(invokeHTTP(connection, method, input, options));
          case "server_streaming":
            return invokeHTTP(connection, method, input, options);
          default:
            return single(invokeWebSocket(connection, method, input, options));
        }
      },
    ]),
  ) as ServiceClient<Service>;
}

export function open({ url, token }: { url: string; token: string }) {
  const connection = { url, token };
  return {
    system: bind(SystemService, connection),
    logs: bind(LogService, connection),
    swiplets: bind(SwipletService, connection),
    repl: bind(REPLService, connection),
  };
}

export type Client = ReturnType<typeof open>;

export const unauthenticated = (error: unknown) =>
  error instanceof RpcError && error.code === Code.Unauthenticated;
