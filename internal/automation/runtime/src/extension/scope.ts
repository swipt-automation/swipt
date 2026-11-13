import type { Console } from "../shared/console";
import {
  any,
  boolean,
  callbackFunction,
  defineGlobals,
  defineNamespace,
  dictionary,
  domString,
  long,
  nullable,
  object,
  optional,
  sequence,
  variadic,
} from "../shared/webidl";
import { Blob, File } from "./blob";
import { Crypto, crypto, CryptoKey, SubtleCrypto } from "./crypto";
import { atob, btoa, TextDecoder, TextEncoder } from "./encoding";
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
import {
  fetch,
  Headers,
  Request,
  requestInfo,
  requestInit,
  Response,
} from "./fetch";
import { FormData } from "./form";
import { reportException } from "./format";
import { native, type Native } from "./native";
import {
  createNavigator,
  performance,
  Performance,
  PerformanceEntry,
  PerformanceMark,
  PerformanceMeasure,
  PerformanceObserver,
  PerformanceObserverEntryList,
  structuredClone,
  WorkerNavigator,
} from "./platform";
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
  TextEncoderStream,
  TransformStream,
  TransformStreamDefaultController,
  WritableStream,
  WritableStreamDefaultController,
  WritableStreamDefaultWriter,
} from "./streams";
import { URL, URLSearchParams } from "./url";
import { WebSocket } from "./websocket";

type Polyfilled =
  | "ReadableStream"
  | "ReadableStreamDefaultReader"
  | "ReadableStreamBYOBReader"
  | "ReadableStreamBYOBRequest"
  | "ReadableStreamDefaultController"
  | "ReadableByteStreamController"
  | "WritableStreamDefaultWriter"
  | "WritableStreamDefaultController"
  | "TransformStreamDefaultController";
type Provided = Omit<typeof Web, Polyfilled> & {
  [Name in Polyfilled]: { prototype: InstanceType<(typeof Web)[Name]> };
};

const timerHandler = (value: unknown, context: string): unknown =>
  typeof value === "function" ? value : domString(value, context);
const timerArguments = [timerHandler, optional(long, 0), variadic(any)];
const clearArguments = [optional(long, 0)];
const structuredSerializeOptions = dictionary({
  transfer: { convert: sequence(object), fallback: [] },
});

const runMicrotask = async (callback: () => void) => {
  await undefined;
  try {
    callback();
  } catch (error) {
    reportException(error);
  }
};

export function installGlobalScope(console: Console): void {
  const evaluate = globalThis.eval;
  const schedule =
    (start: Native["setTimeout"]) =>
    (handler: unknown, timeout: number, ...args: unknown[]) =>
      start(
        typeof handler === "function"
          ? (handler as (...args: unknown[]) => void)
          : () => evaluate(handler as string),
        timeout,
        ...args,
      );
  const navigator = createNavigator(() => native.environment().languages);
  const interfaces = {
    AbortController,
    AbortSignal,
    Event,
    EventTarget,
    CustomEvent,
    ErrorEvent,
    MessageEvent,
    CloseEvent,
    DOMException,
    QuotaExceededError,
    TextEncoder,
    TextDecoder,
    URL,
    URLSearchParams,
    Crypto,
    CryptoKey,
    SubtleCrypto,
    Blob,
    File,
    FormData,
    Headers,
    Request,
    Response,
    WebSocket,
    EventSource,
    ReadableStream,
    ReadableStreamDefaultReader,
    ReadableStreamBYOBReader,
    ReadableStreamBYOBRequest,
    ReadableStreamDefaultController,
    ReadableByteStreamController,
    WritableStream,
    WritableStreamDefaultWriter,
    WritableStreamDefaultController,
    TransformStream,
    TransformStreamDefaultController,
    ByteLengthQueuingStrategy,
    CountQueuingStrategy,
    TextEncoderStream,
    TextDecoderStream,
    CompressionStream,
    DecompressionStream,
    Performance,
    PerformanceEntry,
    PerformanceMark,
    PerformanceMeasure,
    PerformanceObserver,
    PerformanceObserverEntryList,
    WorkerNavigator,
  };
  const operations = {
    atob,
    btoa,
    structuredClone,
    queueMicrotask: (callback: () => void) => {
      void runMicrotask(callback);
    },
    setTimeout: schedule(native.setTimeout) as unknown as typeof setTimeout,
    setInterval: schedule(native.setInterval) as unknown as typeof setInterval,
    clearTimeout: (id?: number) => native.clearTimer(id!),
    clearInterval: (id?: number) => native.clearTimer(id!),
    fetch,
  };
  const attributes = { crypto, performance, navigator };
  void ({
    ...interfaces,
    ...operations,
    ...attributes,
    console,
  } satisfies Provided);
  defineNamespace(console, "console", {
    assert: [optional(boolean, false), variadic(any)],
    clear: [],
    debug: [variadic(any)],
    error: [variadic(any)],
    info: [variadic(any)],
    log: [variadic(any)],
    table: [optional(any), optional(sequence(domString))],
    trace: [variadic(any)],
    warn: [variadic(any)],
    dir: [optional(any), optional(nullable(object))],
    dirxml: [variadic(any)],
    count: [optional(domString, "default")],
    countReset: [optional(domString, "default")],
    group: [variadic(any)],
    groupCollapsed: [variadic(any)],
    groupEnd: [],
    time: [optional(domString, "default")],
    timeLog: [optional(domString, "default"), variadic(any)],
    timeEnd: [optional(domString, "default")],
  });
  defineGlobals(globalThis, {
    interfaces,
    namespaces: { console },
    operations: {
      atob: [atob, [domString]],
      btoa: [btoa, [domString]],
      structuredClone: [
        structuredClone,
        [any, optional(structuredSerializeOptions)],
      ],
      queueMicrotask: [operations.queueMicrotask, [callbackFunction]],
      setTimeout: [operations.setTimeout, timerArguments],
      setInterval: [operations.setInterval, timerArguments],
      clearTimeout: [operations.clearTimeout, clearArguments],
      clearInterval: [operations.clearInterval, clearArguments],
      fetch: [fetch, [requestInfo, optional(requestInit)]],
    },
    attributes: {
      crypto: { value: crypto },
      performance: { value: performance, replaceable: true },
      navigator: { value: navigator },
    },
  });
}
