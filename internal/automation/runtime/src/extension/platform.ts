import { createTransformStream } from "web-streams-polyfill";

import { createStructuredClone } from "../shared/clone";
import { inspectAs } from "../shared/inspect";
import { createPerformance } from "../shared/performance";
import { brand, defineInterface, iterablePairs } from "../shared/webidl";
import { Blob, File } from "./blob";
import { cloneKey, Crypto, CryptoKey, SubtleCrypto } from "./crypto";
import { TextDecoder, TextEncoder } from "./encoding";
import { DOMException, QuotaExceededError } from "./errors";
import {
  AbortController,
  AbortSignal,
  CloseEvent,
  CustomEvent,
  ErrorEvent,
  Event,
  EventTarget,
  MessageEvent,
} from "./events";
import { EventSource } from "./eventsource";
import { Headers, Request, Response } from "./fetch";
import { FormData } from "./form";
import { native } from "./native";
import {
  ByteLengthQueuingStrategy,
  CompressionStream,
  CountQueuingStrategy,
  DecompressionStream,
  ReadableByteStreamController,
  ReadableStream,
  ReadableStreamBYOBReader,
  ReadableStreamBYOBRequest,
  ReadableStreamDefaultController,
  ReadableStreamDefaultReader,
  TextDecoderStream,
  internalSlots,
  TextEncoderStream,
  TransformStream,
  TransformStreamDefaultController,
  WritableStream,
  WritableStreamDefaultController,
  WritableStreamDefaultWriter,
} from "./streams";
import { inspectURL, URL, URLSearchParams } from "./url";
import { WebSocket } from "./websocket";

const timing = createPerformance({
  now: () => native.now(),
  get timeOrigin() {
    return Number(native.timeOrigin);
  },
  EventTarget,
  DOMException,
  structuredClone: (value) => structuredClone(value),
});

export const {
  performance,
  Performance,
  PerformanceEntry,
  PerformanceMark,
  PerformanceMeasure,
  PerformanceObserver,
  PerformanceObserverEntryList,
} = timing;

const creating = Symbol("WorkerNavigator");
export let createNavigator: (
  languages: () => readonly string[],
) => WorkerNavigator;

export const WorkerNavigator = defineInterface(
  class WorkerNavigator {
    readonly #languages: () => readonly string[];
    #frozen: readonly string[] = Object.freeze([]);

    static [brand](value: object) {
      return #languages in value;
    }

    static {
      createNavigator = (languages) => new WorkerNavigator(creating, languages);
    }

    constructor(
      token?: typeof creating,
      languages: () => readonly string[] = () => [],
    ) {
      if (token !== creating) {
        throw new TypeError("Illegal constructor");
      }
      this.#languages = languages;
    }

    get userAgent(): string {
      return "Swipt";
    }

    get language(): string {
      return this.#languages()[0] ?? "en";
    }

    get languages(): readonly string[] {
      const languages = this.#languages();
      if (
        languages.length !== this.#frozen.length ||
        languages.some((language, index) => language !== this.#frozen[index])
      ) {
        this.#frozen = Object.freeze([...languages]);
      }
      return this.#frozen;
    }
  },
  {},
);
export type WorkerNavigator = InstanceType<typeof WorkerNavigator>;

const methodOf = <T>(target: object, name: PropertyKey) => {
  const method = Object.getOwnPropertyDescriptor(target, name)!;
  return (value: object, ...args: unknown[]) =>
    Reflect.apply(
      (method.get ?? method.value) as (...args: unknown[]) => unknown,
      value,
      args,
    ) as T;
};
const readableLocked = methodOf<boolean>(ReadableStream.prototype, "locked");
const writableLocked = methodOf<boolean>(WritableStream.prototype, "locked");
const pipeTo = methodOf<Promise<void>>(ReadableStream.prototype, "pipeTo");
const transformReadable = methodOf<ReadableStream>(
  TransformStream.prototype,
  "readable",
);
const transformWritable = methodOf<WritableStream>(
  TransformStream.prototype,
  "writable",
);

const locked = (name: string) =>
  new DOMException(
    `a locked ${name} could not be transferred`,
    "DataCloneError",
  );
const cloning = () =>
  new TransformStream({
    transform: (chunk, controller) =>
      controller.enqueue(structuredClone(chunk)),
  });
const pipe = (source: ReadableStream, destination: WritableStream) => {
  void pipeTo(source, destination).then(undefined, () => {});
};

const transferReadable = (stream: ReadableStream) => {
  const channel = cloning();
  return [
    channel.readable,
    () => {
      if (readableLocked(stream)) {
        throw locked("ReadableStream");
      }
      pipe(stream, channel.writable);
    },
  ] as const;
};

const transferWritable = (stream: WritableStream) => {
  const channel = cloning();
  return [
    channel.writable,
    () => {
      if (writableLocked(stream)) {
        throw locked("WritableStream");
      }
      pipe(channel.readable, stream);
    },
  ] as const;
};

const transferTransform = (stream: TransformStream) => {
  const readable = transformReadable(stream);
  const writable = transformWritable(stream);
  const [receivedReadable, completeReadable] = transferReadable(readable);
  const [receivedWritable, completeWritable] = transferWritable(writable);
  return [
    createTransformStream(receivedReadable, receivedWritable),
    () => {
      if (readableLocked(readable) || writableLocked(writable)) {
        throw locked("TransformStream");
      }
      completeReadable();
      completeWritable();
    },
  ] as const;
};

export const structuredClone = createStructuredClone(DOMException, [
  {
    type: DOMException,
    clone: (error: DOMException) => new DOMException(error.message, error.name),
  },
  {
    type: QuotaExceededError,
    clone: (error: QuotaExceededError) =>
      new QuotaExceededError(error.message, {
        quota: error.quota ?? undefined,
        requested: error.requested ?? undefined,
      }),
  },
  {
    type: File,
    clone: (file: File) =>
      new File([file], file.name, {
        type: file.type,
        lastModified: file.lastModified,
      }),
  },
  { type: Blob, clone: (blob: Blob) => new Blob([blob], { type: blob.type }) },
  { type: CryptoKey, clone: cloneKey },
  { type: ReadableStream, transfer: transferReadable },
  { type: WritableStream, transfer: transferWritable },
  { type: TransformStream, transfer: transferTransform },
  ...[
    EventTarget,
    Event,
    AbortController,
    Crypto,
    SubtleCrypto,
    FormData,
    Headers,
    Request,
    Response,
    TextEncoder,
    TextDecoder,
    URL,
    URLSearchParams,
    ReadableStreamDefaultReader,
    ReadableStreamBYOBReader,
    ReadableStreamBYOBRequest,
    ReadableStreamDefaultController,
    ReadableByteStreamController,
    WritableStreamDefaultWriter,
    WritableStreamDefaultController,
    TransformStreamDefaultController,
    ByteLengthQueuingStrategy,
    CountQueuingStrategy,
    TextEncoderStream,
    TextDecoderStream,
    CompressionStream,
    DecompressionStream,
    PerformanceEntry,
    PerformanceObserver,
    PerformanceObserverEntryList,
  ].map((type) => ({ type })),
]);

const slots = (value: object) =>
  internalSlots(value) as {
    _state?: string;
    _ownerReadableStream?: ReadableStream;
    _ownerWritableStream?: WritableStream;
  };

inspectAs(Event, [
  "type",
  "bubbles",
  "cancelable",
  "composed",
  "defaultPrevented",
  "eventPhase",
  "isTrusted",
  "target",
  "currentTarget",
  "timeStamp",
]);
inspectAs(CustomEvent, ["detail"]);
inspectAs(ErrorEvent, ["message", "filename", "lineno", "colno", "error"]);
inspectAs(AbortSignal, ["aborted", "reason"]);
inspectAs(AbortController, ["signal"]);
inspectAs(Blob, ["size", "type"]);
inspectAs(File, ["name", "lastModified"]);
inspectAs(
  FormData,
  (form) =>
    iterablePairs(FormData, form) as Iterable<readonly [string, unknown]>,
);
inspectAs(
  Headers,
  (headers) =>
    iterablePairs(Headers, headers) as Iterable<readonly [string, unknown]>,
);
inspectAs(Request, ["method", "url", "headers", "body", "bodyUsed"]);
inspectAs(Response, [
  "status",
  "statusText",
  "url",
  "redirected",
  "headers",
  "body",
  "bodyUsed",
]);
inspectAs(MessageEvent, ["data", "origin", "lastEventId"]);
inspectAs(CloseEvent, ["code", "reason", "wasClean"]);
inspectAs(WebSocket, [
  "url",
  "readyState",
  "protocol",
  "extensions",
  "binaryType",
]);
inspectAs(EventSource, ["url", "readyState", "withCredentials"]);
inspectAs(URL, (url) => inspectURL(url));
inspectAs(
  URLSearchParams,
  (params) =>
    iterablePairs(URLSearchParams, params) as Iterable<
      readonly [string, unknown]
    >,
);
inspectAs(TextDecoder, ["encoding", "fatal", "ignoreBOM"]);
inspectAs(CryptoKey, ["type", "extractable", "algorithm", "usages"]);
inspectAs(ReadableStream, (stream) => [
  ["locked", stream.locked],
  ["state", slots(stream)._state],
]);
inspectAs(WritableStream, (stream) => [
  ["locked", stream.locked],
  ["state", slots(stream)._state],
]);
inspectAs(TransformStream, ["readable", "writable"]);
inspectAs(ReadableStreamDefaultReader, (reader) => [
  ["stream", slots(reader)._ownerReadableStream],
]);
inspectAs(ReadableStreamBYOBReader, (reader) => [
  ["stream", slots(reader)._ownerReadableStream],
]);
inspectAs(WritableStreamDefaultWriter, (writer) => [
  ["stream", slots(writer)._ownerWritableStream],
]);
inspectAs(ReadableStreamDefaultController, ["desiredSize"]);
inspectAs(ReadableByteStreamController, ["desiredSize"]);
inspectAs(ReadableStreamBYOBRequest, ["view"]);
inspectAs(WritableStreamDefaultController, ["signal"]);
inspectAs(TransformStreamDefaultController, ["desiredSize"]);
inspectAs(ByteLengthQueuingStrategy, ["highWaterMark"]);
inspectAs(CountQueuingStrategy, ["highWaterMark"]);
inspectAs(TextEncoderStream, ["encoding", "readable", "writable"]);
inspectAs(TextDecoderStream, [
  "encoding",
  "fatal",
  "ignoreBOM",
  "readable",
  "writable",
]);
inspectAs(CompressionStream, ["readable", "writable"]);
inspectAs(DecompressionStream, ["readable", "writable"]);
inspectAs(Performance, ["timeOrigin"]);
inspectAs(PerformanceEntry, ["name", "entryType", "startTime", "duration"]);
inspectAs(PerformanceMark, ["detail"]);
inspectAs(PerformanceMeasure, ["detail"]);
inspectAs(WorkerNavigator, ["userAgent", "language", "languages"]);
