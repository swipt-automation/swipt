import {
  any,
  boolean,
  brand,
  bufferSource,
  defineInterface,
  dictionary,
  domString,
  enumeration,
  implementsInterface,
  instance,
  isBufferSource,
  isObject,
  iterableMethod,
  nullable,
  optional,
  record,
  sequence,
  sequenceFrom,
  unsignedShort,
  usvString,
  type Converter,
} from "../shared/webidl";
import { invoke } from "./async";
import { Blob, blobBytes, objectURL } from "./blob";
import { chunk } from "./chunks";
import {
  atob,
  base64Bytes,
  bytesOf,
  TextDecoder,
  TextEncoder,
} from "./encoding";
import { AbortSignal, dependentSignal, onAbort } from "./events";
import { entryList, FormData } from "./form";
import { native } from "./native";
import {
  bytesStream,
  disturbed,
  errorNative,
  nativeReadable,
  nativeWritable,
  ReadableStream,
  takeNative,
  TextDecoderStream,
  TransformStream,
} from "./streams";
import { URL, URLSearchParams } from "./url";

type HeaderList = [string, string][];
type Guard = "none" | "immutable";

const token = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;
const httpWhitespace = /^[\t\n\r ]+|[\t\n\r ]+$/g;
const invalidValue = /[\0\r\n]/;
const reasonPhrase = /^[\t !-~\x80-\xff]*$/;
const encoder = new TextEncoder();

const byteString: Converter<string> = (value, context) => {
  const text = domString(value, context);
  if (/[^\0-\xff]/.test(text)) {
    throw new TypeError(`${context} has characters above U+00FF`);
  }
  return text;
};

const headerName = (name: string): string => {
  if (!token.test(name)) {
    throw new TypeError(`${JSON.stringify(name)} is not a valid header name`);
  }
  return name.toLowerCase();
};

const headerValue = (value: string): string => {
  const text = value.replace(httpWhitespace, "");
  if (invalidValue.test(text)) {
    throw new TypeError(`${JSON.stringify(text)} is not a valid header value`);
  }
  return text;
};

const headerPair: Converter<[string, string]> = (value, context) => {
  const pair = sequence(byteString)(value, context);
  if (pair.length !== 2) {
    throw new TypeError(`${context} is not a pair of a name and a value`);
  }
  return pair as [string, string];
};

const headerRecord = record(byteString, byteString);

export const headersInit: Converter<HeaderList> = (value, context) => {
  if (!isObject(value)) {
    throw new TypeError(`${context} is not a sequence or a record`);
  }
  const method = iterableMethod(value, context);
  return method === undefined
    ? headerRecord(value, context)
    : sequenceFrom(value, method, headerPair, context);
};

let headerList: (headers: object) => HeaderList;
let headerGuard: (headers: object) => Guard;
let pairsOf: (headers: object) => HeaderList;
let createHeaders: (list: HeaderList, guard: Guard) => globalThis.Headers;

const sortAndCombine = (list: HeaderList): HeaderList => {
  const names = [...new Set(list.map(([name]) => name))].sort();
  const out: HeaderList = [];
  for (const name of names) {
    const values = list
      .filter(([item]) => item === name)
      .map(([, value]) => value);
    if (name === "set-cookie") {
      out.push(...values.map((value): [string, string] => [name, value]));
    } else {
      out.push([name, values.join(", ")]);
    }
  }
  return out;
};

export const Headers = defineInterface(
  class Headers {
    #list: HeaderList = [];
    #guard: Guard = "none";
    #sorted: HeaderList | null = null;
    declare forEach: (
      callback: (
        value: string,
        name: string,
        headers: globalThis.Headers,
      ) => void,
      thisArg?: unknown,
    ) => void;
    declare entries: () => Web.HeadersIterator<[string, string]>;
    declare keys: () => Web.HeadersIterator<string>;
    declare values: () => Web.HeadersIterator<string>;
    declare [Symbol.iterator]: () => Web.HeadersIterator<[string, string]>;

    static [brand](value: object) {
      return #list in value;
    }

    static {
      headerList = (headers) => (headers as Headers).#list;
      headerGuard = (headers) => (headers as Headers).#guard;
      pairsOf = (headers) => (headers as Headers).#pairs;
      createHeaders = (list, guard) => {
        const headers = new Headers();
        headers.#list = list.map(([name, value]) => [name, value]);
        headers.#guard = guard;
        return headers;
      };
    }

    constructor(init?: Web.HeadersInit) {
      for (const [name, value] of (init as HeaderList | undefined) ?? []) {
        this.#append(name, value);
      }
    }

    get #pairs(): HeaderList {
      this.#sorted ??= sortAndCombine(this.#list);
      return this.#sorted;
    }

    #mutable(): void {
      if (this.#guard === "immutable") {
        throw new TypeError("the headers are immutable");
      }
      this.#sorted = null;
    }

    #append(name: string, value: string): void {
      const entry: [string, string] = [headerName(name), headerValue(value)];
      this.#mutable();
      this.#list.push(entry);
    }

    append(name: string, value: string): void {
      this.#append(name, value);
    }

    delete(name: string): void {
      const key = headerName(name);
      this.#mutable();
      this.#list = this.#list.filter(([item]) => item !== key);
    }

    get(name: string): string | null {
      const key = headerName(name);
      const values = this.#list
        .filter(([item]) => item === key)
        .map(([, value]) => value);
      return values.length === 0 ? null : values.join(", ");
    }

    getSetCookie(): string[] {
      return this.#list
        .filter(([name]) => name === "set-cookie")
        .map(([, value]) => value);
    }

    has(name: string): boolean {
      const key = headerName(name);
      return this.#list.some(([item]) => item === key);
    }

    set(name: string, value: string): void {
      const key = headerName(name);
      const text = headerValue(value);
      this.#mutable();
      const index = this.#list.findIndex(([item]) => item === key);
      if (index < 0) {
        this.#list.push([key, text]);
        return;
      }
      this.#list[index] = [key, text];
      this.#list = this.#list.filter(
        ([item], position) => position <= index || item !== key,
      );
    }
  },
  {
    construct: [optional(headersInit)],
    operations: {
      append: [byteString, byteString],
      delete: [byteString],
      get: [byteString],
      getSetCookie: [],
      has: [byteString],
      set: [byteString, byteString],
    },
    iterable: (headers) => pairsOf(headers),
  },
);
export const headersOf = (headers: object): HeaderList => headerList(headers);

type Source = { bytes: Uint8Array } | { stream: ReadableStream<Uint8Array> };

interface BodyState {
  source: Source | null;
  view?: ReadableStream<Uint8Array>;
  used: boolean;
  headers: Headers;
  signal?: AbortSignal;
  release?: () => void;
}

const bodies = new WeakMap<object, BodyState>();
const bodyOf = (target: object): BodyState => bodies.get(target)!;

type BodyInit =
  | ReadableStream<Uint8Array>
  | Blob
  | ArrayBuffer
  | ArrayBufferView
  | FormData
  | URLSearchParams
  | string;

const bodyInit: Converter<BodyInit> = (value, context) => {
  if (value instanceof ReadableStream) {
    return value as ReadableStream<Uint8Array>;
  }
  if (
    implementsInterface(Blob, value) ||
    implementsInterface(FormData, value) ||
    implementsInterface(URLSearchParams, value)
  ) {
    return value as Blob;
  }
  return isBufferSource(value)
    ? bufferSource(value, context)
    : usvString(value, context);
};

const extract = (body: BodyInit): { source: Source; type: string | null } => {
  if (body instanceof ReadableStream) {
    if (disturbed(body) || body.locked) {
      throw new TypeError("the body stream is locked or was read");
    }
    return { source: { stream: body }, type: null };
  }
  if (implementsInterface(Blob, body)) {
    const blob = body as Blob;
    return {
      source: { bytes: blobBytes(blob).slice() },
      type: blob.type === "" ? null : blob.type,
    };
  }
  if (implementsInterface(FormData, body)) {
    const { bytes, type } = chunk("multipart").encode(
      entryList(body as object),
    );
    return { source: { bytes }, type };
  }
  if (implementsInterface(URLSearchParams, body)) {
    return {
      source: { bytes: encoder.encode(String(body)) },
      type: "application/x-www-form-urlencoded;charset=UTF-8",
    };
  }
  if (typeof body === "string") {
    return {
      source: { bytes: encoder.encode(body) },
      type: "text/plain;charset=UTF-8",
    };
  }
  return { source: { bytes: bytesOf(body).slice() }, type: null };
};

const consumedStream = (): ReadableStream<Uint8Array> => {
  const stream = bytesStream(new Uint8Array(0));
  void stream.getReader().read();
  return stream;
};

const streamOf = (state: BodyState): ReadableStream<Uint8Array> | null => {
  const { source } = state;
  if (source === null) {
    return null;
  }
  if ("stream" in source) {
    return source.stream;
  }
  state.view ??= state.used ? consumedStream() : bytesStream(source.bytes);
  return state.view;
};

const exposedStream = (state: BodyState) =>
  state.source !== null && "stream" in state.source
    ? state.source.stream
    : state.view;

const usedOf = (state: BodyState): boolean => {
  const stream = exposedStream(state);
  return (
    state.source !== null &&
    (state.used || (stream !== undefined && disturbed(stream)))
  );
};

const unusable = (state: BodyState): boolean =>
  usedOf(state) || exposedStream(state)?.locked === true;

const markUsed = (state: BodyState): void => {
  state.used = true;
  if (state.view !== undefined && !state.view.locked) {
    void state.view.getReader().read();
  }
};

const concat = (chunks: Uint8Array[], size: number): Uint8Array => {
  const out = new Uint8Array(size);
  let offset = 0;
  for (const piece of chunks) {
    out.set(piece, offset);
    offset += piece.length;
  }
  return out;
};

const readAll = async (
  stream: ReadableStream<Uint8Array>,
): Promise<Uint8Array> => {
  const reader = stream.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) {
      return concat(chunks, size);
    }
    if (!(value instanceof Uint8Array)) {
      const error = new TypeError(
        "the body stream produced a chunk that is not a Uint8Array",
      );
      await reader.cancel(error).catch(() => {});
      throw error;
    }
    chunks.push(value);
    size += value.length;
  }
};

const begin = (state: BodyState): Source | null => {
  state.signal?.throwIfAborted();
  if (unusable(state)) {
    throw new TypeError("the body was already read");
  }
  if (state.source !== null) {
    markUsed(state);
  }
  return state.source;
};

const readBody = async (
  state: BodyState,
  source: Source | null,
): Promise<Uint8Array> => {
  if (source === null) {
    return new Uint8Array(0);
  }
  if ("bytes" in source) {
    return source.bytes;
  }
  const id = takeNative(source.stream);
  if (id === null) {
    return readAll(source.stream);
  }
  try {
    return (
      (await invoke<Uint8Array | null>("stream.readAll", [id], {
        signal: state.signal,
      })) ?? new Uint8Array(0)
    );
  } finally {
    state.release?.();
  }
};

const consume = (state: BodyState): Promise<Uint8Array> =>
  readBody(state, begin(state));

const takeBody = (state: BodyState): Source | null => {
  const { source } = state;
  markUsed(state);
  if (source !== null && "stream" in source) {
    state.source = { stream: consumedStream() };
  }
  return source;
};

const transferBody = (state: BodyState): Source | null => {
  const { source } = state;
  markUsed(state);
  if (source === null || "bytes" in source) {
    return source;
  }
  const id = takeNative(source.stream);
  return {
    stream:
      id === null
        ? source.stream.pipeThrough(new TransformStream())
        : nativeReadable(id),
  };
};

const teeBody = (state: BodyState): Source | null => {
  if (unusable(state)) {
    throw new TypeError("the body was already read");
  }
  const { source } = state;
  if (source === null) {
    return null;
  }
  if ("bytes" in source) {
    return { bytes: source.bytes.slice() };
  }
  const [first, second] = source.stream.tee();
  state.source = { stream: first };
  return { stream: second };
};

const arrayBufferOf = (bytes: Uint8Array): ArrayBuffer =>
  bytes.byteOffset === 0 && bytes.byteLength === bytes.buffer.byteLength
    ? (bytes.buffer as ArrayBuffer)
    : (bytes.slice().buffer as ArrayBuffer);

const decodeText = (bytes: Uint8Array): string =>
  new TextDecoder().decode(bytes);

const bodyMixin = {
  get body(): ReadableStream<Uint8Array> | null {
    return streamOf(bodyOf(this));
  },
  get bodyUsed(): boolean {
    return usedOf(bodyOf(this));
  },
  async arrayBuffer(): Promise<ArrayBuffer> {
    return arrayBufferOf(await consume(bodyOf(this)));
  },
  async blob(): Promise<Blob> {
    const state = bodyOf(this);
    const type = state.headers.get("content-type") ?? "";
    return new Blob([await consume(state)], { type });
  },
  async bytes(): Promise<Uint8Array> {
    return consume(bodyOf(this));
  },
  async formData(): Promise<FormData> {
    const state = bodyOf(this);
    const type = state.headers.get("content-type");
    return chunk("multipart").parse(await consume(state), type);
  },
  async json(): Promise<unknown> {
    return JSON.parse(decodeText(await consume(bodyOf(this))));
  },
  async text(): Promise<string> {
    return decodeText(await consume(bodyOf(this)));
  },
  textStream(): ReadableStream<string> {
    const state = bodyOf(this);
    if (unusable(state)) {
      throw new TypeError("the body was already read");
    }
    const stream = streamOf(state) ?? bytesStream(new Uint8Array(0));
    if (state.source !== null) {
      state.used = true;
    }
    return stream.pipeThrough(new TextDecoderStream());
  },
};

const bodyOperations = {
  arrayBuffer: [],
  blob: [],
  bytes: [],
  formData: [],
  json: [],
  text: [],
  textStream: [],
};

const standardMethods = ["DELETE", "GET", "HEAD", "OPTIONS", "POST", "PUT"];
const forbiddenMethods = ["CONNECT", "TRACE", "TRACK"];

type RequestCache =
  | "default"
  | "no-store"
  | "reload"
  | "no-cache"
  | "force-cache"
  | "only-if-cached";
type RequestCredentials = "omit" | "same-origin" | "include";
type RequestMode = "navigate" | "same-origin" | "no-cors" | "cors";
type RequestPriority = "high" | "low" | "auto";
type RequestRedirect = "follow" | "error" | "manual";
type ReferrerPolicy =
  | ""
  | "no-referrer"
  | "no-referrer-when-downgrade"
  | "same-origin"
  | "origin"
  | "strict-origin"
  | "origin-when-cross-origin"
  | "strict-origin-when-cross-origin"
  | "unsafe-url";

interface RequestInitValues {
  body?: BodyInit | null;
  cache?: RequestCache;
  credentials?: RequestCredentials;
  duplex?: "half";
  headers?: HeaderList;
  integrity?: string;
  keepalive?: boolean;
  method?: string;
  mode?: RequestMode;
  priority?: RequestPriority;
  redirect?: RequestRedirect;
  referrer?: string;
  referrerPolicy?: ReferrerPolicy;
  signal?: AbortSignal | null;
  window?: unknown;
}

export const requestInit = dictionary<RequestInitValues>({
  body: nullable(bodyInit),
  cache: enumeration<RequestCache>([
    "default",
    "no-store",
    "reload",
    "no-cache",
    "force-cache",
    "only-if-cached",
  ]),
  credentials: enumeration<RequestCredentials>([
    "omit",
    "same-origin",
    "include",
  ]),
  duplex: enumeration(["half"]),
  headers: headersInit,
  integrity: domString,
  keepalive: boolean,
  method: byteString,
  mode: enumeration<RequestMode>([
    "navigate",
    "same-origin",
    "no-cors",
    "cors",
  ]),
  priority: enumeration<RequestPriority>(["high", "low", "auto"]),
  redirect: enumeration<RequestRedirect>(["follow", "error", "manual"]),
  referrer: usvString,
  referrerPolicy: enumeration<ReferrerPolicy>([
    "",
    "no-referrer",
    "no-referrer-when-downgrade",
    "same-origin",
    "origin",
    "strict-origin",
    "origin-when-cross-origin",
    "strict-origin-when-cross-origin",
    "unsafe-url",
  ]),
  signal: nullable((value, context): unknown =>
    instance(AbortSignal)(value, context),
  ),
  window: any,
});

export const requestInfo: Converter<globalThis.Request | string> = (
  value,
  context,
) =>
  implementsInterface(Request, value)
    ? (value as globalThis.Request)
    : usvString(value, context);

interface RequestFields {
  url: string;
  method: string;
  headers: Headers;
  signal: AbortSignal;
  cache: RequestCache;
  credentials: RequestCredentials;
  integrity: string;
  keepalive: boolean;
  mode: RequestMode;
  priority: RequestPriority;
  redirect: RequestRedirect;
  referrer: string;
  referrerPolicy: ReferrerPolicy;
}

const internal = Symbol("internal");

let requestFields: (request: object) => RequestFields;
let constructRequest: (
  input: globalThis.Request | string,
  init: RequestInitValues,
) => globalThis.Request;

const parseURL = (input: string, what: string): URL => {
  try {
    return new URL(input);
  } catch {
    throw new TypeError(`${JSON.stringify(input)} is not ${what}`);
  }
};

export const Request = defineInterface(
  class Request {
    readonly #fields: RequestFields;
    declare readonly body: ReadableStream<Uint8Array> | null;
    declare readonly bodyUsed: boolean;
    declare arrayBuffer: () => Promise<ArrayBuffer>;
    declare blob: () => Promise<Blob>;
    declare bytes: () => Promise<Uint8Array>;
    declare formData: () => Promise<FormData>;
    declare json: () => Promise<unknown>;
    declare text: () => Promise<string>;
    declare textStream: () => ReadableStream<string>;

    static [brand](value: object) {
      return #fields in value;
    }

    static {
      requestFields = (request) => (request as Request).#fields;
      constructRequest = (input, init) =>
        new Request(input, init as globalThis.RequestInit);
    }

    constructor(
      input: globalThis.Request | string | globalThis.URL | typeof internal,
      init?: globalThis.RequestInit | RequestFields,
      state?: BodyState,
    ) {
      if (input === internal) {
        this.#fields = init as RequestFields;
        bodies.set(this, state!);
        return;
      }
      const options = init as RequestInitValues;
      let fields: RequestFields;
      let inputBody: BodyState | null = null;
      let signal: AbortSignal | null = null;
      if (typeof input === "string") {
        const url = parseURL(input, "an absolute URL");
        if (url.username !== "" || url.password !== "") {
          throw new TypeError(
            `the URL ${JSON.stringify(input)} includes credentials; send them in an Authorization header`,
          );
        }
        fields = {
          url: url.href,
          method: "GET",
          headers: createHeaders([], "none"),
          signal: dependentSignal([]),
          cache: "default",
          credentials: "same-origin",
          integrity: "",
          keepalive: false,
          mode: "cors",
          priority: "auto",
          redirect: "follow",
          referrer: "about:client",
          referrerPolicy: "",
        };
      } else {
        fields = { ...(input as unknown as Request).#fields };
        signal = fields.signal;
        inputBody = bodyOf(input as object);
      }
      if (options.window !== undefined && options.window !== null) {
        throw new TypeError("a Request's window can only be null");
      }
      if (options.mode === "navigate") {
        throw new TypeError("a Request can't be made with the mode navigate");
      }
      if (Object.keys(options).length > 0 && fields.mode === "navigate") {
        fields.mode = "same-origin";
      }
      if (options.referrer !== undefined) {
        fields.referrer =
          options.referrer === ""
            ? ""
            : parseURL(options.referrer, "a valid referrer URL").href;
      }
      fields.referrerPolicy = options.referrerPolicy ?? fields.referrerPolicy;
      fields.mode = options.mode ?? fields.mode;
      fields.credentials = options.credentials ?? fields.credentials;
      fields.cache = options.cache ?? fields.cache;
      if (fields.cache === "only-if-cached" && fields.mode !== "same-origin") {
        throw new TypeError(
          'a Request with the cache only-if-cached needs the mode "same-origin"',
        );
      }
      fields.redirect = options.redirect ?? fields.redirect;
      fields.integrity = options.integrity ?? fields.integrity;
      fields.keepalive = options.keepalive ?? fields.keepalive;
      fields.priority = options.priority ?? fields.priority;
      if (options.method !== undefined) {
        const method = options.method;
        if (!token.test(method)) {
          throw new TypeError(
            `${JSON.stringify(method)} is not a valid method`,
          );
        }
        const upper = method.toUpperCase();
        if (forbiddenMethods.includes(upper)) {
          throw new TypeError(`the method ${upper} is forbidden`);
        }
        fields.method = standardMethods.includes(upper) ? upper : method;
      }
      if (
        fields.mode === "no-cors" &&
        !["GET", "HEAD", "POST"].includes(fields.method)
      ) {
        throw new TypeError(
          `a no-cors request's method is GET, HEAD or POST, not ${fields.method}`,
        );
      }
      if (options.signal !== undefined) {
        signal = options.signal;
      }
      fields.signal = dependentSignal(signal === null ? [] : [signal]);
      const headers = createHeaders([], "none");
      for (const [name, value] of options.headers ??
        headerList(fields.headers)) {
        headers.append(name, value);
      }
      fields.headers = headers;
      const initBody = options.body ?? null;
      const hasInputBody = inputBody !== null && inputBody.source !== null;
      if (
        (initBody !== null || hasInputBody) &&
        (fields.method === "GET" || fields.method === "HEAD")
      ) {
        throw new TypeError(`a ${fields.method} request can't have a body`);
      }
      let source: Source | null = null;
      if (initBody !== null) {
        const extracted = extract(initBody);
        if ("stream" in extracted.source && options.duplex !== "half") {
          throw new TypeError(
            'a request with a ReadableStream body needs duplex: "half"',
          );
        }
        if (extracted.type !== null && !fields.headers.has("content-type")) {
          fields.headers.append("content-type", extracted.type);
        }
        source = extracted.source;
      } else if (hasInputBody) {
        if (unusable(inputBody!)) {
          throw new TypeError("the input request's body was already read");
        }
        source = transferBody(inputBody!);
      }
      this.#fields = fields;
      bodies.set(this, { source, used: false, headers: fields.headers });
    }

    get method(): string {
      return this.#fields.method;
    }

    get url(): string {
      return this.#fields.url;
    }

    get headers(): Headers {
      return this.#fields.headers;
    }

    get destination(): "" {
      return "";
    }

    get referrer(): string {
      return this.#fields.referrer;
    }

    get referrerPolicy(): ReferrerPolicy {
      return this.#fields.referrerPolicy;
    }

    get mode(): RequestMode {
      return this.#fields.mode;
    }

    get credentials(): RequestCredentials {
      return this.#fields.credentials;
    }

    get cache(): RequestCache {
      return this.#fields.cache;
    }

    get redirect(): RequestRedirect {
      return this.#fields.redirect;
    }

    get integrity(): string {
      return this.#fields.integrity;
    }

    get keepalive(): boolean {
      return this.#fields.keepalive;
    }

    get isReloadNavigation(): boolean {
      return false;
    }

    get isHistoryNavigation(): boolean {
      return false;
    }

    get signal(): AbortSignal {
      return this.#fields.signal;
    }

    get duplex(): "half" {
      return "half";
    }

    clone(): Request {
      const state = bodyOf(this);
      const source = teeBody(state);
      const headers = createHeaders(headerList(this.#fields.headers), "none");
      const fields = {
        ...this.#fields,
        headers,
        signal: dependentSignal([this.#fields.signal]),
      };
      return new Request(internal, fields, { source, used: false, headers });
    }
  },
  {
    construct: [requestInfo, optional(requestInit)],
    operations: { clone: [], ...bodyOperations },
    include: [bodyMixin],
  },
);

type ResponseType =
  "basic" | "cors" | "default" | "error" | "opaque" | "opaqueredirect";

interface ResponseInitValues {
  headers?: HeaderList;
  status: number;
  statusText: string;
}

const responseInit = dictionary<ResponseInitValues>({
  headers: headersInit,
  status: { convert: unsignedShort, fallback: 200 },
  statusText: { convert: byteString, fallback: "" },
});

interface ResponseFields {
  type: ResponseType;
  url: string;
  redirected: boolean;
  status: number;
  statusText: string;
  headers: Headers;
}

const nullBodyStatuses = [101, 103, 204, 205, 304];
const redirectStatuses = [301, 302, 303, 307, 308];

let createResponse: (
  fields: ResponseFields,
  state: BodyState,
) => globalThis.Response;

const initialize = (
  init: ResponseInitValues,
  body: { source: Source; type: string | null } | null,
): [ResponseFields, BodyState] => {
  if (init.status < 200 || init.status > 599) {
    throw new RangeError(`the status ${init.status} is outside 200 to 599`);
  }
  if (!reasonPhrase.test(init.statusText)) {
    throw new TypeError(
      `${JSON.stringify(init.statusText)} is not a valid status text`,
    );
  }
  const headers = createHeaders([], "none");
  for (const [name, value] of init.headers ?? []) {
    headers.append(name, value);
  }
  if (body !== null && nullBodyStatuses.includes(init.status)) {
    throw new TypeError(
      `a response with the status ${init.status} can't have a body`,
    );
  }
  if (body?.type != null && !headers.has("content-type")) {
    headers.append("content-type", body.type);
  }
  return [
    {
      type: "default",
      url: "",
      redirected: false,
      status: init.status,
      statusText: init.statusText,
      headers,
    },
    { source: body?.source ?? null, used: false, headers },
  ];
};

export const Response = defineInterface(
  class Response {
    readonly #fields: ResponseFields;
    declare readonly body: ReadableStream<Uint8Array> | null;
    declare readonly bodyUsed: boolean;
    declare arrayBuffer: () => Promise<ArrayBuffer>;
    declare blob: () => Promise<Blob>;
    declare bytes: () => Promise<Uint8Array>;
    declare formData: () => Promise<FormData>;
    declare json: () => Promise<unknown>;
    declare text: () => Promise<string>;
    declare textStream: () => ReadableStream<string>;

    static [brand](value: object) {
      return #fields in value;
    }

    static {
      createResponse = (fields, state) => new Response(internal, fields, state);
    }

    constructor(
      body?: Web.BodyInit | null | typeof internal,
      init?: globalThis.ResponseInit | ResponseFields,
      state?: BodyState,
    ) {
      if (body === internal) {
        this.#fields = init as ResponseFields;
        bodies.set(this, state!);
        return;
      }
      const [fields, created] = initialize(
        init as ResponseInitValues,
        body === null ? null : extract(body as BodyInit),
      );
      this.#fields = fields;
      bodies.set(this, created);
    }

    static error(): Response {
      const headers = createHeaders([], "immutable");
      return new Response(
        internal,
        {
          type: "error",
          url: "",
          redirected: false,
          status: 0,
          statusText: "",
          headers,
        },
        { source: null, used: false, headers },
      );
    }

    static json(data: unknown, init?: globalThis.ResponseInit): Response {
      const text = JSON.stringify(data);
      if (text === undefined) {
        throw new TypeError("the data can't be serialized to JSON");
      }
      const [fields, state] = initialize(init as ResponseInitValues, {
        source: { bytes: encoder.encode(text) },
        type: "application/json",
      });
      return new Response(internal, fields, state);
    }

    static redirect(url: string | globalThis.URL, status?: number): Response {
      const parsed = parseURL(url as string, "an absolute URL");
      if (!redirectStatuses.includes(status!)) {
        throw new RangeError(`${status} is not a redirect status`);
      }
      const headers = createHeaders([["location", parsed.href]], "immutable");
      return new Response(
        internal,
        {
          type: "default",
          url: "",
          redirected: false,
          status,
          statusText: "",
          headers,
        },
        { source: null, used: false, headers },
      );
    }

    get type(): ResponseType {
      return this.#fields.type;
    }

    get url(): string {
      return this.#fields.url;
    }

    get redirected(): boolean {
      return this.#fields.redirected;
    }

    get status(): number {
      return this.#fields.status;
    }

    get ok(): boolean {
      return this.#fields.status >= 200 && this.#fields.status <= 299;
    }

    get statusText(): string {
      return this.#fields.statusText;
    }

    get headers(): Headers {
      return this.#fields.headers;
    }

    clone(): Response {
      const state = bodyOf(this);
      const source = teeBody(state);
      const { headers } = this.#fields;
      const copied = createHeaders(headerList(headers), headerGuard(headers));
      return new Response(
        internal,
        { ...this.#fields, headers: copied },
        {
          source,
          used: false,
          headers: copied,
          signal: state.signal,
        },
      );
    }
  },
  {
    construct: [optional(nullable(bodyInit), null), optional(responseInit)],
    operations: { clone: [], ...bodyOperations },
    statics: {
      error: [],
      json: [any, optional(responseInit)],
      redirect: [usvString, optional(unsignedShort, 302)],
    },
    include: [bodyMixin],
  },
);

const basicResponse = (
  url: string,
  status: number,
  statusText: string,
  list: HeaderList,
  source: Source | null,
  extra: Partial<BodyState> & { redirected?: boolean } = {},
): Response => {
  const headers = createHeaders(list, "immutable");
  const { redirected = false, ...state } = extra;
  return createResponse(
    { type: "basic", url, redirected, status, statusText, headers },
    { source, used: false, headers, ...state },
  );
};

const percentDecode = (text: string): Uint8Array => {
  const bytes = encoder.encode(text);
  const out = new Uint8Array(bytes.length);
  let written = 0;
  const hex = (byte: number) =>
    byte >= 0x30 && byte <= 0x39
      ? byte - 0x30
      : (byte | 0x20) >= 0x61 && (byte | 0x20) <= 0x66
        ? (byte | 0x20) - 0x57
        : -1;
  for (let index = 0; index < bytes.length; index++) {
    const high = bytes[index] === 0x25 ? hex(bytes[index + 1]) : -1;
    const low = high >= 0 ? hex(bytes[index + 2]) : -1;
    if (low >= 0) {
      out[written++] = (high << 4) | low;
      index += 2;
    } else {
      out[written++] = bytes[index];
    }
  }
  return out.slice(0, written);
};

const withoutFragment = (href: string): string => href.replace(/#.*$/s, "");

const mimeType = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+\/[!#$%&'*+\-.^_`|~0-9A-Za-z]+/;

const dataResponse = (href: string, method: string): Response => {
  const url = withoutFragment(href);
  const content = url.slice("data:".length);
  const comma = content.indexOf(",");
  if (comma < 0) {
    throw new TypeError("the data: URL has no comma");
  }
  let type = content
    .slice(0, comma)
    .replace(/^[\t\n\f\r ]+|[\t\n\f\r ]+$/g, "");
  let bytes = percentDecode(content.slice(comma + 1));
  const base64 = /;[\t\n\f\r ]*base64[\t\n\f\r ]*$/i.exec(type);
  if (base64) {
    type = type.slice(0, base64.index);
    let decoded: string;
    try {
      decoded = atob(decodeText(bytes));
    } catch {
      throw new TypeError("the data: URL is not valid base64");
    }
    bytes = Uint8Array.from(decoded, (character) => character.charCodeAt(0));
  }
  type = type.replace(/[\t\n\f\r ;]+$/, "");
  if (type.startsWith(";")) {
    type = `text/plain${type}`;
  }
  if (!mimeType.test(type)) {
    type = "text/plain;charset=US-ASCII";
  }
  return basicResponse(
    url,
    200,
    "OK",
    [["content-type", type]],
    method === "HEAD" ? null : { bytes },
  );
};

const blobResponse = (href: string, method: string): Response => {
  const url = withoutFragment(href);
  const blob = objectURL(url);
  if (blob === undefined) {
    throw new TypeError(`the blob URL ${url} was revoked or never created`);
  }
  if (method !== "GET") {
    throw new TypeError("a blob URL can only be fetched with GET");
  }
  return basicResponse(
    url,
    200,
    "OK",
    [
      ["content-type", mimeType.test(blob.type) ? blob.type : ""],
      ["content-length", String(blob.size)],
    ],
    { bytes: blobBytes(blob).slice() },
  );
};

export interface Transport {
  readonly proxy?: string;
  readonly ca?: readonly string[];
  readonly rejectUnauthorized?: boolean;
  readonly cookies?: {
    readonly id?: string;
    readonly root?: string;
    readonly path?: string;
  };
}

interface Fetched {
  status: number;
  statusText: string;
  url: string;
  redirected: boolean;
  headers: HeaderList | null;
  body: string;
}

const uploadChunk = (chunk: unknown): Uint8Array => {
  if (!(chunk instanceof Uint8Array)) {
    throw new TypeError(
      "the request body stream produced a chunk that is not a Uint8Array",
    );
  }
  return chunk;
};

export const throwIfAborted = (request: globalThis.Request): void => {
  const { signal } = requestFields(request);
  if (!signal.aborted) {
    return;
  }
  const { source } = bodyOf(request);
  if (source !== null && "stream" in source && !source.stream.locked) {
    source.stream.cancel(signal.reason).catch(() => {});
  }
  throw signal.reason;
};

export const requestOf = (
  input: unknown,
  init: unknown,
  context: string,
): Request =>
  constructRequest(
    requestInfo(input, `${context} argument 1`),
    requestInit(init, `${context} argument 2`),
  );

const integrityHashes = ["sha256", "sha384", "sha512"];

const matchesIntegrity = (bytes: Uint8Array, integrity: string): boolean => {
  let strongest = -1;
  let expected: string[] = [];
  for (const item of integrity.split(/[\t\n\f\r ]+/)) {
    const [expression] = item.split("?");
    const dash = expression.indexOf("-");
    const rank =
      dash < 0
        ? -1
        : integrityHashes.indexOf(expression.slice(0, dash).toLowerCase());
    if (rank < 0 || rank < strongest) {
      continue;
    }
    if (rank > strongest) {
      strongest = rank;
      expected = [];
    }
    expected.push(
      expression
        .slice(dash + 1)
        .replace(/-/g, "+")
        .replace(/_/g, "/")
        .replace(/=+$/, ""),
    );
  }
  if (strongest < 0) {
    return true;
  }
  const algorithm = `SHA-${integrityHashes[strongest].slice(3)}`;
  const actual = base64Bytes(native.digest(algorithm, bytes));
  return expected.includes(actual.replace(/=+$/, ""));
};

async function send(request: Request, transport: Transport): Promise<Response> {
  const response = await transmit(request, transport);
  const { integrity } = requestFields(request);
  if (integrity === "") {
    return response;
  }
  const state = bodyOf(response);
  if (state.source === null) {
    throw new TypeError("the response has no body to check the integrity of");
  }
  const bytes = await consume(state);
  if (!matchesIntegrity(bytes, integrity)) {
    throw new TypeError(
      `the response from ${response.url} doesn't match the integrity metadata`,
    );
  }
  return basicResponse(
    response.url,
    response.status,
    response.statusText,
    headerList(response.headers),
    { bytes },
    { redirected: response.redirected },
  );
}

async function transmit(
  request: Request,
  transport: Transport,
): Promise<Response> {
  const fields = requestFields(request);
  const state = bodyOf(request);
  const { signal } = fields;
  signal.throwIfAborted();
  const protocol = fields.url.slice(0, fields.url.indexOf(":") + 1);
  switch (protocol) {
    case "data:":
      return dataResponse(fields.url, fields.method);
    case "blob:":
      return blobResponse(fields.url, fields.method);
    case "http:":
    case "https:":
      break;
    default:
      throw new TypeError(
        `fetch supports http:, https:, data: and blob: URLs, not ${protocol}`,
      );
  }
  const source = takeBody(state);
  let body = "";
  let stream = "";
  if (source !== null && "bytes" in source) {
    body = native.put(source.bytes);
  } else if (source !== null) {
    const id = takeNative(source.stream);
    if (id !== null) {
      stream = id;
    } else {
      const [reader, writer] = await invoke<[string, string]>(
        "stream.create",
        [],
      );
      stream = reader;
      source.stream
        .pipeTo(nativeWritable(writer, uploadChunk), { signal })
        .catch(() => {});
    }
  }
  const headers = [...headerList(fields.headers)];
  const has = (name: string) => headers.some(([item]) => item === name);
  if (fields.cache === "no-store" || fields.cache === "reload") {
    if (!has("pragma")) {
      headers.push(["pragma", "no-cache"]);
    }
    if (!has("cache-control")) {
      headers.push(["cache-control", "no-cache"]);
    }
  } else if (fields.cache === "no-cache" && !has("cache-control")) {
    headers.push(["cache-control", "max-age=0"]);
  }
  let fetched: Fetched;
  try {
    fetched = await invoke<Fetched>(
      "fetch",
      [
        {
          url: fields.url,
          method: fields.method,
          headers,
          body,
          stream,
          redirect: fields.redirect,
          credentials: fields.credentials,
        },
        transport,
      ],
      { signal },
    );
  } catch (error) {
    if (stream !== "") {
      native.release(stream, "the request failed");
    }
    throw error;
  }
  if (signal.aborted) {
    if (fetched.body !== "") {
      native.release(fetched.body);
    }
    throw signal.reason;
  }
  const list = (fetched.headers ?? []).map(
    ([name, value]): [string, string] => [name.toLowerCase(), value],
  );
  const url = new URL(fetched.url).href;
  if (fetched.body === "") {
    return basicResponse(url, fetched.status, fetched.statusText, list, null, {
      redirected: fetched.redirected,
    });
  }
  const release = onAbort(signal, () => errorNative(readable, signal.reason));
  const readable = nativeReadable(fetched.body, release);
  return basicResponse(
    url,
    fetched.status,
    fetched.statusText,
    list,
    { stream: readable },
    { redirected: fetched.redirected, signal, release },
  );
}

export interface Connection {
  readonly headers: readonly (readonly [string, string])[];
  readonly transport: Transport;
}

export const defaultConnection: Connection = Object.freeze({
  headers: [],
  transport: {},
});

export function sendWith(
  request: globalThis.Request,
  connection: Connection,
): Promise<Response> {
  const present = new Set(headerList(request.headers).map(([name]) => name));
  for (const [name, value] of connection.headers) {
    if (!present.has(name)) {
      request.headers.append(name, value);
    }
  }
  return send(request, connection.transport);
}

export async function fetch(
  input: globalThis.Request | string | globalThis.URL,
  init?: globalThis.RequestInit,
): Promise<Response> {
  const request = constructRequest(
    input as globalThis.Request | string,
    init as RequestInitValues,
  );
  throwIfAborted(request);
  return send(request, {});
}
