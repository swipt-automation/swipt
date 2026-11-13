declare namespace Web {
  type Fallback<T> = typeof globalThis extends { onmessage: any } ? {} : T;
  type FallbackValue<Name extends string, T> = typeof globalThis extends {
    onmessage: any;
  }
    ? typeof globalThis extends Record<Name, infer U>
      ? U
      : never
    : T;
  type WorkerFallback<T> = typeof globalThis extends { importScripts: any }
    ? {}
    : T;
  type WorkerFallbackValue<Name extends string, T> = typeof globalThis extends {
    importScripts: any;
  }
    ? FallbackValue<Name, T>
    : T;

  interface WorkerNavigator {
    readonly userAgent: string;
    readonly language: string;
    readonly languages: ReadonlyArray<string>;
  }
  var WorkerNavigator: abstract new () => WorkerNavigator;
  var navigator: WorkerNavigator;
  interface AbortController {
    readonly signal: AbortSignal;
    abort(reason?: any): void;
  }
  var AbortController: {
    prototype: AbortController;
    new (): AbortController;
  };
  interface AbortSignal extends EventTarget {
    readonly aborted: boolean;
    onabort: ((this: AbortSignal, ev: Event) => any) | null;
    readonly reason: any;
    throwIfAborted(): void;
    addEventListener<K extends keyof AbortSignalEventMap>(
      type: K,
      listener: (this: AbortSignal, ev: AbortSignalEventMap[K]) => any,
      options?: boolean | AddEventListenerOptions,
    ): void;
    addEventListener(
      type: string,
      listener: EventListenerOrEventListenerObject,
      options?: boolean | AddEventListenerOptions,
    ): void;
    removeEventListener<K extends keyof AbortSignalEventMap>(
      type: K,
      listener: (this: AbortSignal, ev: AbortSignalEventMap[K]) => any,
      options?: boolean | EventListenerOptions,
    ): void;
    removeEventListener(
      type: string,
      listener: EventListenerOrEventListenerObject,
      options?: boolean | EventListenerOptions,
    ): void;
  }
  var AbortSignal: {
    prototype: AbortSignal;
    new (): AbortSignal;
    abort(reason?: any): AbortSignal;
    any(signals: AbortSignal[]): AbortSignal;
    timeout(milliseconds: number): AbortSignal;
  };
  interface Event {
    readonly bubbles: boolean;
    cancelBubble: boolean;
    readonly cancelable: boolean;
    readonly composed: boolean;
    readonly currentTarget: EventTarget | null;
    readonly defaultPrevented: boolean;
    readonly eventPhase: number;
    readonly isTrusted: boolean;
    returnValue: boolean;
    readonly srcElement: EventTarget | null;
    readonly target: EventTarget | null;
    readonly timeStamp: DOMHighResTimeStamp;
    readonly type: string;
    composedPath(): EventTarget[];
    initEvent(type: string, bubbles?: boolean, cancelable?: boolean): void;
    preventDefault(): void;
    stopImmediatePropagation(): void;
    stopPropagation(): void;
    readonly NONE: 0;
    readonly CAPTURING_PHASE: 1;
    readonly AT_TARGET: 2;
    readonly BUBBLING_PHASE: 3;
  }
  var Event: {
    prototype: Event;
    new (type: string, eventInitDict?: EventInit): Event;
    readonly NONE: 0;
    readonly CAPTURING_PHASE: 1;
    readonly AT_TARGET: 2;
    readonly BUBBLING_PHASE: 3;
  };
  interface EventTarget {
    addEventListener(
      type: string,
      callback: EventListenerOrEventListenerObject | null,
      options?: AddEventListenerOptions | boolean,
    ): void;
    dispatchEvent(event: Event): boolean;
    removeEventListener(
      type: string,
      callback: EventListenerOrEventListenerObject | null,
      options?: EventListenerOptions | boolean,
    ): void;
  }
  var EventTarget: {
    prototype: EventTarget;
    new (): EventTarget;
  };
  interface CustomEvent<T = any> extends Event {
    readonly detail: T | null;
    initCustomEvent(
      type: string,
      bubbles?: boolean,
      cancelable?: boolean,
      detail?: T,
    ): void;
  }
  var CustomEvent: {
    prototype: CustomEvent;
    new <T>(type: string, eventInitDict?: CustomEventInit<T>): CustomEvent<T>;
  };
  interface ErrorEvent extends Event {
    readonly colno: number;
    readonly error: any;
    readonly filename: string;
    readonly lineno: number;
    readonly message: string;
  }
  var ErrorEvent: {
    prototype: ErrorEvent;
    new (type: string, eventInitDict?: ErrorEventInit): ErrorEvent;
  };
  interface DOMException extends Error {
    readonly code: number;
    readonly message: string;
    readonly name: string;
    readonly INDEX_SIZE_ERR: 1;
    readonly DOMSTRING_SIZE_ERR: 2;
    readonly HIERARCHY_REQUEST_ERR: 3;
    readonly WRONG_DOCUMENT_ERR: 4;
    readonly INVALID_CHARACTER_ERR: 5;
    readonly NO_DATA_ALLOWED_ERR: 6;
    readonly NO_MODIFICATION_ALLOWED_ERR: 7;
    readonly NOT_FOUND_ERR: 8;
    readonly NOT_SUPPORTED_ERR: 9;
    readonly INUSE_ATTRIBUTE_ERR: 10;
    readonly INVALID_STATE_ERR: 11;
    readonly SYNTAX_ERR: 12;
    readonly INVALID_MODIFICATION_ERR: 13;
    readonly NAMESPACE_ERR: 14;
    readonly INVALID_ACCESS_ERR: 15;
    readonly VALIDATION_ERR: 16;
    readonly TYPE_MISMATCH_ERR: 17;
    readonly SECURITY_ERR: 18;
    readonly NETWORK_ERR: 19;
    readonly ABORT_ERR: 20;
    readonly URL_MISMATCH_ERR: 21;
    readonly QUOTA_EXCEEDED_ERR: 22;
    readonly TIMEOUT_ERR: 23;
    readonly INVALID_NODE_TYPE_ERR: 24;
    readonly DATA_CLONE_ERR: 25;
  }
  var DOMException: {
    prototype: DOMException;
    new (message?: string, name?: string): DOMException;
    readonly INDEX_SIZE_ERR: 1;
    readonly DOMSTRING_SIZE_ERR: 2;
    readonly HIERARCHY_REQUEST_ERR: 3;
    readonly WRONG_DOCUMENT_ERR: 4;
    readonly INVALID_CHARACTER_ERR: 5;
    readonly NO_DATA_ALLOWED_ERR: 6;
    readonly NO_MODIFICATION_ALLOWED_ERR: 7;
    readonly NOT_FOUND_ERR: 8;
    readonly NOT_SUPPORTED_ERR: 9;
    readonly INUSE_ATTRIBUTE_ERR: 10;
    readonly INVALID_STATE_ERR: 11;
    readonly SYNTAX_ERR: 12;
    readonly INVALID_MODIFICATION_ERR: 13;
    readonly NAMESPACE_ERR: 14;
    readonly INVALID_ACCESS_ERR: 15;
    readonly VALIDATION_ERR: 16;
    readonly TYPE_MISMATCH_ERR: 17;
    readonly SECURITY_ERR: 18;
    readonly NETWORK_ERR: 19;
    readonly ABORT_ERR: 20;
    readonly URL_MISMATCH_ERR: 21;
    readonly QUOTA_EXCEEDED_ERR: 22;
    readonly TIMEOUT_ERR: 23;
    readonly INVALID_NODE_TYPE_ERR: 24;
    readonly DATA_CLONE_ERR: 25;
  };
  interface QuotaExceededError extends DOMException {
    readonly quota: number | null;
    readonly requested: number | null;
  }
  interface QuotaExceededErrorOptions {
    quota?: number;
    requested?: number;
  }
  var QuotaExceededError: {
    prototype: QuotaExceededError;
    new (
      message?: string,
      options?: QuotaExceededErrorOptions,
    ): QuotaExceededError;
  } & Omit<typeof DOMException, "prototype">;
  interface TextEncoder extends TextEncoderCommon {
    encode(input?: string): Uint8Array;
    encodeInto(
      source: string,
      destination: Uint8Array,
    ): TextEncoderEncodeIntoResult;
  }
  var TextEncoder: {
    prototype: TextEncoder;
    new (): TextEncoder;
  };
  interface TextDecoder extends TextDecoderCommon {
    decode(
      input?: AllowSharedBufferSource,
      options?: TextDecodeOptions,
    ): string;
  }
  var TextDecoder: {
    prototype: TextDecoder;
    new (label?: string, options?: TextDecoderOptions): TextDecoder;
  };
  function atob(data: string): string;
  function btoa(data: string): string;
  function structuredClone<T = any>(
    value: T,
    options?: StructuredSerializeOptions,
  ): T;
  interface URL {
    hash: string;
    host: string;
    hostname: string;
    href: string;
    toString(): string;
    readonly origin: string;
    password: string;
    pathname: string;
    port: string;
    protocol: string;
    search: string;
    readonly searchParams: URLSearchParams;
    username: string;
    toJSON(): string;
  }
  var URL: {
    prototype: URL;
    new (url: string | URL, base?: string | URL): URL;
    canParse(url: string | URL, base?: string | URL): boolean;
    createObjectURL(obj: Blob): string;
    parse(url: string | URL, base?: string | URL): URL | null;
    revokeObjectURL(url: string): void;
  };
  interface URLSearchParams {
    readonly size: number;
    append(name: string, value: string): void;
    delete(name: string, value?: string): void;
    get(name: string): string | null;
    getAll(name: string): string[];
    has(name: string, value?: string): boolean;
    set(name: string, value: string): void;
    sort(): void;
    toString(): string;
    forEach(
      callbackfn: (value: string, key: string, parent: URLSearchParams) => void,
      thisArg?: any,
    ): void;
    [Symbol.iterator](): URLSearchParamsIterator<[string, string]>;
    entries(): URLSearchParamsIterator<[string, string]>;
    keys(): URLSearchParamsIterator<string>;
    values(): URLSearchParamsIterator<string>;
  }
  var URLSearchParams: {
    prototype: URLSearchParams;
    new (
      init?: string[][] | Record<string, string> | string | URLSearchParams,
    ): URLSearchParams;
  };
  interface Crypto {
    readonly subtle: SubtleCrypto;
    getRandomValues<T extends Exclude<BufferSource, ArrayBuffer>>(array: T): T;
    randomUUID(): `${string}-${string}-${string}-${string}-${string}`;
  }
  var Crypto: abstract new () => Crypto;
  interface CryptoKey {
    readonly algorithm: KeyAlgorithm;
    readonly extractable: boolean;
    readonly type: KeyType;
    readonly usages: KeyUsage[];
  }
  var CryptoKey: abstract new () => CryptoKey;
  interface SubtleCrypto {
    decrypt(
      algorithm:
        | AlgorithmIdentifier
        | RsaOaepParams
        | AesCtrParams
        | AesCbcParams
        | AesGcmParams,
      key: CryptoKey,
      data: BufferSource,
    ): Promise<ArrayBuffer>;
    deriveBits(
      algorithm:
        AlgorithmIdentifier | EcdhKeyDeriveParams | HkdfParams | Pbkdf2Params,
      baseKey: CryptoKey,
      length?: number | null,
    ): Promise<ArrayBuffer>;
    deriveKey(
      algorithm:
        AlgorithmIdentifier | EcdhKeyDeriveParams | HkdfParams | Pbkdf2Params,
      baseKey: CryptoKey,
      derivedKeyType:
        | AlgorithmIdentifier
        | AesDerivedKeyParams
        | HmacImportParams
        | HkdfParams
        | Pbkdf2Params,
      extractable: boolean,
      keyUsages: KeyUsage[],
    ): Promise<CryptoKey>;
    digest(
      algorithm: AlgorithmIdentifier,
      data: BufferSource,
    ): Promise<ArrayBuffer>;
    encrypt(
      algorithm:
        | AlgorithmIdentifier
        | RsaOaepParams
        | AesCtrParams
        | AesCbcParams
        | AesGcmParams,
      key: CryptoKey,
      data: BufferSource,
    ): Promise<ArrayBuffer>;
    exportKey(format: "jwk", key: CryptoKey): Promise<JsonWebKey>;
    exportKey(
      format: Exclude<KeyFormat, "jwk">,
      key: CryptoKey,
    ): Promise<ArrayBuffer>;
    exportKey(
      format: KeyFormat,
      key: CryptoKey,
    ): Promise<ArrayBuffer | JsonWebKey>;
    generateKey(
      algorithm: "Ed25519" | { name: "Ed25519" },
      extractable: boolean,
      keyUsages: ReadonlyArray<"sign" | "verify">,
    ): Promise<CryptoKeyPair>;
    generateKey(
      algorithm: "X25519" | { name: "X25519" },
      extractable: boolean,
      keyUsages: ReadonlyArray<"deriveBits" | "deriveKey">,
    ): Promise<CryptoKeyPair>;
    generateKey(
      algorithm: RsaHashedKeyGenParams | EcKeyGenParams,
      extractable: boolean,
      keyUsages: ReadonlyArray<KeyUsage>,
    ): Promise<CryptoKeyPair>;
    generateKey(
      algorithm: AesKeyGenParams | HmacKeyGenParams | Pbkdf2Params,
      extractable: boolean,
      keyUsages: ReadonlyArray<KeyUsage>,
    ): Promise<CryptoKey>;
    generateKey(
      algorithm: AlgorithmIdentifier,
      extractable: boolean,
      keyUsages: KeyUsage[],
    ): Promise<CryptoKeyPair | CryptoKey>;
    importKey(
      format: "jwk",
      keyData: JsonWebKey,
      algorithm:
        | AlgorithmIdentifier
        | RsaHashedImportParams
        | EcKeyImportParams
        | HmacImportParams
        | AesKeyAlgorithm,
      extractable: boolean,
      keyUsages: ReadonlyArray<KeyUsage>,
    ): Promise<CryptoKey>;
    importKey(
      format: Exclude<KeyFormat, "jwk">,
      keyData: BufferSource,
      algorithm:
        | AlgorithmIdentifier
        | RsaHashedImportParams
        | EcKeyImportParams
        | HmacImportParams
        | AesKeyAlgorithm,
      extractable: boolean,
      keyUsages: KeyUsage[],
    ): Promise<CryptoKey>;
    sign(
      algorithm: AlgorithmIdentifier | RsaPssParams | EcdsaParams,
      key: CryptoKey,
      data: BufferSource,
    ): Promise<ArrayBuffer>;
    unwrapKey(
      format: KeyFormat,
      wrappedKey: BufferSource,
      unwrappingKey: CryptoKey,
      unwrapAlgorithm:
        | AlgorithmIdentifier
        | RsaOaepParams
        | AesCtrParams
        | AesCbcParams
        | AesGcmParams,
      unwrappedKeyAlgorithm:
        | AlgorithmIdentifier
        | RsaHashedImportParams
        | EcKeyImportParams
        | HmacImportParams
        | AesKeyAlgorithm,
      extractable: boolean,
      keyUsages: KeyUsage[],
    ): Promise<CryptoKey>;
    verify(
      algorithm: AlgorithmIdentifier | RsaPssParams | EcdsaParams,
      key: CryptoKey,
      signature: BufferSource,
      data: BufferSource,
    ): Promise<boolean>;
    wrapKey(
      format: KeyFormat,
      key: CryptoKey,
      wrappingKey: CryptoKey,
      wrapAlgorithm:
        | AlgorithmIdentifier
        | RsaOaepParams
        | AesCtrParams
        | AesCbcParams
        | AesGcmParams,
    ): Promise<ArrayBuffer>;
    deriveKey(
      algorithm:
        AlgorithmIdentifier | EcdhKeyDeriveParams | HkdfParams | Pbkdf2Params,
      baseKey: CryptoKey,
      derivedKeyType:
        | AlgorithmIdentifier
        | AesDerivedKeyParams
        | HmacImportParams
        | HkdfParams
        | Pbkdf2Params,
      extractable: boolean,
      keyUsages: Iterable<KeyUsage>,
    ): Promise<CryptoKey>;
    generateKey(
      algorithm: AlgorithmIdentifier,
      extractable: boolean,
      keyUsages: Iterable<KeyUsage>,
    ): Promise<CryptoKeyPair | CryptoKey>;
    importKey(
      format: Exclude<KeyFormat, "jwk">,
      keyData: BufferSource,
      algorithm:
        | AlgorithmIdentifier
        | RsaHashedImportParams
        | EcKeyImportParams
        | HmacImportParams
        | AesKeyAlgorithm,
      extractable: boolean,
      keyUsages: Iterable<KeyUsage>,
    ): Promise<CryptoKey>;
    unwrapKey(
      format: KeyFormat,
      wrappedKey: BufferSource,
      unwrappingKey: CryptoKey,
      unwrapAlgorithm:
        | AlgorithmIdentifier
        | RsaOaepParams
        | AesCtrParams
        | AesCbcParams
        | AesGcmParams,
      unwrappedKeyAlgorithm:
        | AlgorithmIdentifier
        | RsaHashedImportParams
        | EcKeyImportParams
        | HmacImportParams
        | AesKeyAlgorithm,
      extractable: boolean,
      keyUsages: Iterable<KeyUsage>,
    ): Promise<CryptoKey>;
  }
  var SubtleCrypto: abstract new () => SubtleCrypto;
  var crypto: Crypto;
  interface Blob {
    readonly size: number;
    readonly type: string;
    arrayBuffer(): Promise<ArrayBuffer>;
    bytes(): Promise<Uint8Array>;
    slice(start?: number, end?: number, contentType?: string): Blob;
    stream(): ReadableStream<Uint8Array>;
    text(): Promise<string>;
    textStream(): ReadableStream<string>;
  }
  var Blob: {
    prototype: Blob;
    new (blobParts?: BlobPart[], options?: BlobPropertyBag): Blob;
  };
  interface File extends Blob {
    readonly lastModified: number;
    readonly name: string;
    readonly webkitRelativePath: string;
  }
  var File: {
    prototype: File;
    new (
      fileBits: BlobPart[],
      fileName: string,
      options?: FilePropertyBag,
    ): File;
  };
  interface FormData {
    append(name: string, value: string | Blob): void;
    append(name: string, value: string): void;
    append(name: string, blobValue: Blob, filename?: string): void;
    delete(name: string): void;
    get(name: string): FormDataEntryValue | null;
    getAll(name: string): FormDataEntryValue[];
    has(name: string): boolean;
    set(name: string, value: string | Blob): void;
    set(name: string, value: string): void;
    set(name: string, blobValue: Blob, filename?: string): void;
    forEach(
      callbackfn: (
        value: FormDataEntryValue,
        key: string,
        parent: FormData,
      ) => void,
      thisArg?: any,
    ): void;
    [Symbol.iterator](): FormDataIterator<[string, FormDataEntryValue]>;
    entries(): FormDataIterator<[string, FormDataEntryValue]>;
    keys(): FormDataIterator<string>;
    values(): FormDataIterator<FormDataEntryValue>;
  }
  var FormData: {
    prototype: FormData;
    new (): FormData;
  };
  interface ReadableStream<R = any> {
    readonly locked: boolean;
    cancel(reason?: any): Promise<void>;
    getReader(options: { mode: "byob" }): ReadableStreamBYOBReader;
    getReader(): ReadableStreamDefaultReader<R>;
    getReader(
      options?: ReadableStreamGetReaderOptions,
    ): ReadableStreamReader<R>;
    pipeThrough<T>(
      transform: ReadableWritablePair<T, R>,
      options?: StreamPipeOptions,
    ): ReadableStream<T>;
    pipeTo(
      destination: WritableStream<R>,
      options?: StreamPipeOptions,
    ): Promise<void>;
    tee(): [ReadableStream<R>, ReadableStream<R>];
    [Symbol.asyncIterator](
      options?: ReadableStreamIteratorOptions,
    ): ReadableStreamAsyncIterator<R>;
    values(
      options?: ReadableStreamIteratorOptions,
    ): ReadableStreamAsyncIterator<R>;
  }
  var ReadableStream: {
    prototype: ReadableStream;
    new (
      underlyingSource: UnderlyingByteSource,
      strategy?: { highWaterMark?: number },
    ): ReadableStream<Uint8Array>;
    new <R = any>(
      underlyingSource: UnderlyingDefaultSource<R>,
      strategy?: QueuingStrategy<R>,
    ): ReadableStream<R>;
    new <R = any>(
      underlyingSource?: UnderlyingSource<R>,
      strategy?: QueuingStrategy<R>,
    ): ReadableStream<R>;
  };
  interface ReadableStreamDefaultReader<
    R = any,
  > extends ReadableStreamGenericReader {
    read(): Promise<ReadableStreamReadResult<R>>;
    releaseLock(): void;
  }
  var ReadableStreamDefaultReader: {
    prototype: ReadableStreamDefaultReader;
    new <R = any>(stream: ReadableStream<R>): ReadableStreamDefaultReader<R>;
  };
  interface ReadableStreamBYOBReader extends ReadableStreamGenericReader {
    read<T extends Exclude<BufferSource, ArrayBuffer>>(
      view: T,
      options?: ReadableStreamBYOBReaderReadOptions,
    ): Promise<ReadableStreamReadResult<T>>;
    releaseLock(): void;
  }
  var ReadableStreamBYOBReader: {
    prototype: ReadableStreamBYOBReader;
    new (stream: ReadableStream<Uint8Array>): ReadableStreamBYOBReader;
  };
  interface ReadableStreamBYOBRequest {
    readonly view: ArrayBufferView | null;
    respond(bytesWritten: number): void;
    respondWithNewView(view: ArrayBufferView): void;
  }
  var ReadableStreamBYOBRequest: abstract new () => ReadableStreamBYOBRequest;
  interface ReadableStreamDefaultController<R = any> {
    readonly desiredSize: number | null;
    close(): void;
    enqueue(chunk: R): void;
    error(e?: any): void;
  }
  var ReadableStreamDefaultController: abstract new () => ReadableStreamDefaultController;
  interface ReadableByteStreamController {
    readonly byobRequest: ReadableStreamBYOBRequest | null;
    readonly desiredSize: number | null;
    close(): void;
    enqueue(chunk: ArrayBufferView): void;
    error(e?: any): void;
  }
  var ReadableByteStreamController: abstract new () => ReadableByteStreamController;
  interface WritableStream<W = any> {
    readonly locked: boolean;
    abort(reason?: any): Promise<void>;
    close(): Promise<void>;
    getWriter(): WritableStreamDefaultWriter<W>;
  }
  var WritableStream: {
    prototype: WritableStream;
    new <W = any>(
      underlyingSink?: UnderlyingSink<W>,
      strategy?: QueuingStrategy<W>,
    ): WritableStream<W>;
  };
  interface WritableStreamDefaultWriter<W = any> {
    readonly closed: Promise<undefined>;
    readonly desiredSize: number | null;
    readonly ready: Promise<void>;
    abort(reason?: any): Promise<void>;
    close(): Promise<void>;
    releaseLock(): void;
    write(chunk?: W): Promise<void>;
  }
  var WritableStreamDefaultWriter: {
    prototype: WritableStreamDefaultWriter;
    new <W = any>(stream: WritableStream<W>): WritableStreamDefaultWriter<W>;
  };
  interface WritableStreamDefaultController {
    readonly signal: AbortSignal;
    error(e?: any): void;
  }
  var WritableStreamDefaultController: abstract new () => WritableStreamDefaultController;
  interface TransformStream<I = any, O = any> {
    readonly readable: ReadableStream<O>;
    readonly writable: WritableStream<I>;
  }
  var TransformStream: {
    prototype: TransformStream;
    new <I = any, O = any>(
      transformer?: Transformer<I, O>,
      writableStrategy?: QueuingStrategy<I>,
      readableStrategy?: QueuingStrategy<O>,
    ): TransformStream<I, O>;
  };
  interface TransformStreamDefaultController<O = any> {
    readonly desiredSize: number | null;
    enqueue(chunk?: O): void;
    error(reason?: any): void;
    terminate(): void;
  }
  var TransformStreamDefaultController: abstract new () => TransformStreamDefaultController;
  interface ByteLengthQueuingStrategy extends QueuingStrategy<ArrayBufferView> {
    readonly highWaterMark: number;
    readonly size: QueuingStrategySize<ArrayBufferView>;
  }
  var ByteLengthQueuingStrategy: {
    prototype: ByteLengthQueuingStrategy;
    new (init: QueuingStrategyInit): ByteLengthQueuingStrategy;
  };
  interface CountQueuingStrategy extends QueuingStrategy {
    readonly highWaterMark: number;
    readonly size: QueuingStrategySize;
  }
  var CountQueuingStrategy: {
    prototype: CountQueuingStrategy;
    new (init: QueuingStrategyInit): CountQueuingStrategy;
  };
  interface TextEncoderStream
    extends GenericTransformStream, TextEncoderCommon {
    readonly readable: ReadableStream<Uint8Array>;
    readonly writable: WritableStream<string>;
  }
  var TextEncoderStream: {
    prototype: TextEncoderStream;
    new (): TextEncoderStream;
  };
  interface TextDecoderStream
    extends GenericTransformStream, TextDecoderCommon {
    readonly readable: ReadableStream<string>;
    readonly writable: WritableStream<BufferSource>;
  }
  var TextDecoderStream: {
    prototype: TextDecoderStream;
    new (label?: string, options?: TextDecoderOptions): TextDecoderStream;
  };
  interface CompressionStream extends GenericTransformStream {
    readonly readable: ReadableStream<Uint8Array>;
    readonly writable: WritableStream<BufferSource>;
  }
  var CompressionStream: {
    prototype: CompressionStream;
    new (format: CompressionFormat): CompressionStream;
  };
  interface DecompressionStream extends GenericTransformStream {
    readonly readable: ReadableStream<Uint8Array>;
    readonly writable: WritableStream<BufferSource>;
  }
  var DecompressionStream: {
    prototype: DecompressionStream;
    new (format: CompressionFormat): DecompressionStream;
  };
  var performance: Performance;
  interface Performance extends EventTarget {
    readonly timeOrigin: DOMHighResTimeStamp;
    clearMarks(markName?: string): void;
    clearMeasures(measureName?: string): void;
    getEntries(): PerformanceEntryList;
    getEntriesByName(name: string, type?: string): PerformanceEntryList;
    getEntriesByType(type: string): PerformanceEntryList;
    mark(
      markName: string,
      markOptions?: PerformanceMarkOptions,
    ): PerformanceMark;
    measure(
      measureName: string,
      startOrMeasureOptions?: string | PerformanceMeasureOptions,
      endMark?: string,
    ): PerformanceMeasure;
    now(): DOMHighResTimeStamp;
    toJSON(): any;
    addEventListener(
      type: string,
      listener: EventListenerOrEventListenerObject,
      options?: boolean | AddEventListenerOptions,
    ): void;
    removeEventListener(
      type: string,
      listener: EventListenerOrEventListenerObject,
      options?: boolean | EventListenerOptions,
    ): void;
  }
  var Performance: abstract new () => Performance;
  interface PerformanceEntry {
    readonly duration: DOMHighResTimeStamp;
    readonly entryType: string;
    readonly name: string;
    readonly startTime: DOMHighResTimeStamp;
    toJSON(): any;
  }
  var PerformanceEntry: abstract new () => PerformanceEntry;
  interface PerformanceMark extends PerformanceEntry {
    readonly detail: any;
  }
  var PerformanceMark: {
    prototype: PerformanceMark;
    new (
      markName: string,
      markOptions?: PerformanceMarkOptions,
    ): PerformanceMark;
  };
  interface PerformanceMeasure extends PerformanceEntry {
    readonly detail: any;
  }
  var PerformanceMeasure: abstract new () => PerformanceMeasure;
  interface PerformanceObserver {
    disconnect(): void;
    observe(options?: PerformanceObserverInit): void;
    takeRecords(): PerformanceEntryList;
  }
  var PerformanceObserver: {
    prototype: PerformanceObserver;
    new (callback: PerformanceObserverCallback): PerformanceObserver;
    readonly supportedEntryTypes: ReadonlyArray<string>;
  };
  interface PerformanceObserverEntryList {
    getEntries(): PerformanceEntryList;
    getEntriesByName(name: string, type?: string): PerformanceEntryList;
    getEntriesByType(type: string): PerformanceEntryList;
  }
  var PerformanceObserverEntryList: abstract new () => PerformanceObserverEntryList;
  function queueMicrotask(callback: VoidFunction): void;
  var console: Console;
  function setTimeout(
    handler: string | ((...args: any[]) => void),
    timeout?: number,
    ...arguments: any[]
  ): number;
  function clearTimeout(id?: number): void;
  function setInterval(
    handler: string | ((...args: any[]) => void),
    timeout?: number,
    ...arguments: any[]
  ): number;
  function clearInterval(id?: number): void;
  interface AbortSignalEventMap {
    abort: Event;
  }
  interface AddEventListenerOptions extends EventListenerOptions {
    once?: boolean;
    passive?: boolean;
    signal?: AbortSignal;
  }
  type EventListenerOrEventListenerObject = EventListener | EventListenerObject;
  interface EventListenerOptions {
    capture?: boolean;
  }
  type DOMHighResTimeStamp = number;
  interface EventInit {
    bubbles?: boolean;
    cancelable?: boolean;
    composed?: boolean;
  }
  interface CustomEventInit<T = any> extends EventInit {
    detail?: T;
  }
  interface ErrorEventInit extends EventInit {
    colno?: number;
    error?: any;
    filename?: string;
    lineno?: number;
    message?: string;
  }
  interface TextEncoderCommon {
    readonly encoding: string;
  }
  interface TextEncoderEncodeIntoResult {
    read: number;
    written: number;
  }
  interface TextDecoderCommon {
    readonly encoding: string;
    readonly fatal: boolean;
    readonly ignoreBOM: boolean;
  }
  type AllowSharedBufferSource = ArrayBufferLike | ArrayBufferView;
  interface TextDecodeOptions {
    stream?: boolean;
  }
  interface TextDecoderOptions {
    fatal?: boolean;
    ignoreBOM?: boolean;
  }
  interface StructuredSerializeOptions {
    transfer?: Iterable<
      ArrayBuffer | ReadableStream | WritableStream | TransformStream
    >;
  }
  interface URLSearchParamsIterator<T> extends IteratorObject<
    T,
    BuiltinIteratorReturn,
    unknown
  > {
    [Symbol.iterator](): URLSearchParamsIterator<T>;
  }
  type BufferSource = ArrayBufferView | ArrayBuffer;
  interface KeyAlgorithm {
    name: string;
  }
  type KeyType = "private" | "public" | "secret";
  type KeyUsage =
    | "decrypt"
    | "deriveBits"
    | "deriveKey"
    | "encrypt"
    | "sign"
    | "unwrapKey"
    | "verify"
    | "wrapKey";
  type AlgorithmIdentifier = Algorithm | string;
  interface RsaOaepParams extends Algorithm {
    label?: BufferSource;
  }
  interface AesCtrParams extends Algorithm {
    counter: BufferSource;
    length: number;
  }
  interface AesCbcParams extends Algorithm {
    iv: BufferSource;
  }
  interface AesGcmParams extends Algorithm {
    additionalData?: BufferSource;
    iv: BufferSource;
    tagLength?: number;
  }
  interface EcdhKeyDeriveParams extends Algorithm {
    public: CryptoKey;
  }
  interface HkdfParams extends Algorithm {
    hash: HashAlgorithmIdentifier;
    info: BufferSource;
    salt: BufferSource;
  }
  interface Pbkdf2Params extends Algorithm {
    hash: HashAlgorithmIdentifier;
    iterations: number;
    salt: BufferSource;
  }
  interface AesDerivedKeyParams extends Algorithm {
    length: number;
  }
  interface HmacImportParams extends Algorithm {
    hash: HashAlgorithmIdentifier;
    length?: number;
  }
  interface JsonWebKey {
    alg?: string;
    crv?: string;
    d?: string;
    dp?: string;
    dq?: string;
    e?: string;
    ext?: boolean;
    k?: string;
    key_ops?: string[];
    kty?: string;
    n?: string;
    oth?: RsaOtherPrimesInfo[];
    p?: string;
    q?: string;
    qi?: string;
    use?: string;
    x?: string;
    y?: string;
  }
  type KeyFormat = "jwk" | "pkcs8" | "raw" | "spki";
  interface CryptoKeyPair {
    privateKey: CryptoKey;
    publicKey: CryptoKey;
  }
  interface RsaHashedKeyGenParams extends RsaKeyGenParams {
    hash: HashAlgorithmIdentifier;
  }
  interface EcKeyGenParams extends Algorithm {
    namedCurve: NamedCurve;
  }
  interface AesKeyGenParams extends Algorithm {
    length: number;
  }
  interface HmacKeyGenParams extends Algorithm {
    hash: HashAlgorithmIdentifier;
    length?: number;
  }
  interface RsaHashedImportParams extends Algorithm {
    hash: HashAlgorithmIdentifier;
  }
  interface EcKeyImportParams extends Algorithm {
    namedCurve: NamedCurve;
  }
  interface AesKeyAlgorithm extends KeyAlgorithm {
    length: number;
  }
  interface RsaPssParams extends Algorithm {
    saltLength: number;
  }
  interface EcdsaParams extends Algorithm {
    hash: HashAlgorithmIdentifier;
  }
  type BlobPart = BufferSource | Blob | string;
  interface BlobPropertyBag {
    endings?: EndingType;
    type?: string;
  }
  interface FilePropertyBag extends BlobPropertyBag {
    lastModified?: number;
  }
  type FormDataEntryValue = File | string;
  interface FormDataIterator<T> extends IteratorObject<
    T,
    BuiltinIteratorReturn,
    unknown
  > {
    [Symbol.iterator](): FormDataIterator<T>;
  }
  interface ReadableStreamGetReaderOptions {
    mode?: ReadableStreamReaderMode;
  }
  type ReadableStreamReader<T> =
    ReadableStreamDefaultReader<T> | ReadableStreamBYOBReader;
  interface ReadableWritablePair<R = any, W = any> {
    readable: ReadableStream<R>;
    writable: WritableStream<W>;
  }
  interface StreamPipeOptions {
    preventAbort?: boolean;
    preventCancel?: boolean;
    preventClose?: boolean;
    signal?: AbortSignal;
  }
  interface UnderlyingByteSource {
    autoAllocateChunkSize?: number;
    cancel?: UnderlyingSourceCancelCallback;
    pull?: (
      controller: ReadableByteStreamController,
    ) => void | PromiseLike<void>;
    start?: (controller: ReadableByteStreamController) => any;
    type: "bytes";
  }
  interface UnderlyingDefaultSource<R = any> {
    cancel?: UnderlyingSourceCancelCallback;
    pull?: (
      controller: ReadableStreamDefaultController<R>,
    ) => void | PromiseLike<void>;
    start?: (controller: ReadableStreamDefaultController<R>) => any;
    type?: undefined;
  }
  interface QueuingStrategy<T = any> {
    highWaterMark?: number;
    size?: QueuingStrategySize<T>;
  }
  interface UnderlyingSource<R = any> {
    autoAllocateChunkSize?: number;
    cancel?: UnderlyingSourceCancelCallback;
    pull?: UnderlyingSourcePullCallback<R>;
    start?: UnderlyingSourceStartCallback<R>;
    type?: ReadableStreamType;
  }
  interface ReadableStreamIteratorOptions {
    preventCancel?: boolean;
  }
  interface ReadableStreamAsyncIterator<T> extends AsyncIteratorObject<
    T,
    BuiltinIteratorReturn,
    unknown
  > {
    [Symbol.asyncIterator](): ReadableStreamAsyncIterator<T>;
  }
  interface ReadableStreamGenericReader {
    readonly closed: Promise<undefined>;
    cancel(reason?: any): Promise<void>;
  }
  type ReadableStreamReadResult<T> =
    ReadableStreamReadValueResult<T> | ReadableStreamReadDoneResult<T>;
  interface ReadableStreamBYOBReaderReadOptions {
    min?: number;
  }
  interface UnderlyingSink<W = any> {
    abort?: UnderlyingSinkAbortCallback;
    close?: UnderlyingSinkCloseCallback;
    start?: UnderlyingSinkStartCallback;
    type?: undefined;
    write?: UnderlyingSinkWriteCallback<W>;
  }
  interface Transformer<I = any, O = any> {
    flush?: TransformerFlushCallback<O>;
    readableType?: undefined;
    start?: TransformerStartCallback<O>;
    transform?: TransformerTransformCallback<I, O>;
    writableType?: undefined;
  }
  interface QueuingStrategySize<T = any> {
    (chunk: T): number;
  }
  interface QueuingStrategyInit {
    highWaterMark: number;
  }
  interface GenericTransformStream {
    readonly readable: ReadableStream;
    readonly writable: WritableStream;
  }
  type CompressionFormat = "brotli" | "deflate" | "deflate-raw" | "gzip";
  type PerformanceEntryList = PerformanceEntry[];
  interface PerformanceMarkOptions {
    detail?: any;
    startTime?: DOMHighResTimeStamp;
  }
  interface PerformanceMeasureOptions {
    detail?: any;
    duration?: DOMHighResTimeStamp;
    end?: string | DOMHighResTimeStamp;
    start?: string | DOMHighResTimeStamp;
  }
  interface PerformanceObserverInit {
    buffered?: boolean;
    entryTypes?: string[];
    type?: string;
  }
  interface PerformanceObserverCallback {
    (
      entries: PerformanceObserverEntryList,
      observer: PerformanceObserver,
    ): void;
  }
  interface VoidFunction {
    (): void;
  }
  interface Console {
    assert(condition?: boolean, ...data: any[]): void;
    clear(): void;
    count(label?: string): void;
    countReset(label?: string): void;
    debug(...data: any[]): void;
    dir(item?: any, options?: any): void;
    dirxml(...data: any[]): void;
    error(...data: any[]): void;
    group(...data: any[]): void;
    groupCollapsed(...data: any[]): void;
    groupEnd(): void;
    info(...data: any[]): void;
    log(...data: any[]): void;
    table(tabularData?: any, properties?: string[]): void;
    time(label?: string): void;
    timeEnd(label?: string): void;
    timeLog(label?: string, ...data: any[]): void;
    trace(...data: any[]): void;
    warn(...data: any[]): void;
  }
  interface EventListener {
    (evt: Event): void;
  }
  interface EventListenerObject {
    handleEvent(object: Event): void;
  }
  interface Algorithm {
    name: string;
  }
  type HashAlgorithmIdentifier = AlgorithmIdentifier;
  interface RsaOtherPrimesInfo {
    d?: string;
    r?: string;
    t?: string;
  }
  interface RsaKeyGenParams extends Algorithm {
    modulusLength: number;
    publicExponent: BigInteger;
  }
  type NamedCurve = string;
  type EndingType = "native" | "transparent";
  type ReadableStreamReaderMode = "byob";
  interface UnderlyingSourceCancelCallback {
    (reason?: any): void | PromiseLike<void>;
  }
  interface UnderlyingSourcePullCallback<R> {
    (controller: ReadableStreamController<R>): void | PromiseLike<void>;
  }
  interface UnderlyingSourceStartCallback<R> {
    (controller: ReadableStreamController<R>): any;
  }
  type ReadableStreamType = "bytes";
  interface ReadableStreamReadValueResult<T> {
    done: false;
    value: T;
  }
  interface ReadableStreamReadDoneResult<T> {
    done: true;
    value: T | undefined;
  }
  interface UnderlyingSinkAbortCallback {
    (reason?: any): void | PromiseLike<void>;
  }
  interface UnderlyingSinkCloseCallback {
    (): void | PromiseLike<void>;
  }
  interface UnderlyingSinkStartCallback {
    (controller: WritableStreamDefaultController): any;
  }
  interface UnderlyingSinkWriteCallback<W> {
    (
      chunk: W,
      controller: WritableStreamDefaultController,
    ): void | PromiseLike<void>;
  }
  interface TransformerFlushCallback<O> {
    (controller: TransformStreamDefaultController<O>): void | PromiseLike<void>;
  }
  interface TransformerStartCallback<O> {
    (controller: TransformStreamDefaultController<O>): any;
  }
  interface TransformerTransformCallback<I, O> {
    (
      chunk: I,
      controller: TransformStreamDefaultController<O>,
    ): void | PromiseLike<void>;
  }
  type BigInteger = Uint8Array;
  type ReadableStreamController<T> =
    ReadableStreamDefaultController<T> | ReadableByteStreamController;
  interface Headers {
    append(name: string, value: string): void;
    delete(name: string): void;
    get(name: string): string | null;
    getSetCookie(): string[];
    has(name: string): boolean;
    set(name: string, value: string): void;
    forEach(
      callbackfn: (value: string, key: string, parent: Headers) => void,
      thisArg?: any,
    ): void;
    [Symbol.iterator](): HeadersIterator<[string, string]>;
    entries(): HeadersIterator<[string, string]>;
    keys(): HeadersIterator<string>;
    values(): HeadersIterator<string>;
  }
  var Headers: {
    prototype: Headers;
    new (init?: HeadersInit): Headers;
  };
  interface Body {
    readonly body: ReadableStream<Uint8Array> | null;
    readonly bodyUsed: boolean;
    arrayBuffer(): Promise<ArrayBuffer>;
    blob(): Promise<Blob>;
    bytes(): Promise<Uint8Array>;
    formData(): Promise<FormData>;
    json(): Promise<any>;
    text(): Promise<string>;
    textStream(): ReadableStream<string>;
  }
  interface Request extends Body {
    readonly cache: RequestCache;
    readonly credentials: RequestCredentials;
    readonly destination: RequestDestination;
    readonly headers: Headers;
    readonly integrity: string;
    readonly keepalive: boolean;
    readonly method: string;
    readonly mode: RequestMode;
    readonly redirect: RequestRedirect;
    readonly referrer: string;
    readonly referrerPolicy: ReferrerPolicy;
    readonly signal: AbortSignal;
    readonly url: string;
    clone(): Request;
  }
  var Request: {
    prototype: Request;
    new (input: RequestInfo | URL, init?: RequestInit): Request;
  };
  interface Response extends Body {
    readonly headers: Headers;
    readonly ok: boolean;
    readonly redirected: boolean;
    readonly status: number;
    readonly statusText: string;
    readonly type: ResponseType;
    readonly url: string;
    clone(): Response;
  }
  var Response: {
    prototype: Response;
    new (body?: BodyInit | null, init?: ResponseInit): Response;
    error(): Response;
    json(data: any, init?: ResponseInit): Response;
    redirect(url: string | URL, status?: number): Response;
  };
  function fetch(
    input: RequestInfo | URL,
    init?: RequestInit,
  ): Promise<Response>;
  interface MessageEvent<T = any> extends Event {
    readonly data: T;
    readonly lastEventId: string;
    readonly origin: string;
    readonly ports: ReadonlyArray<object>;
    readonly source: object | null;
    initMessageEvent(
      type: string,
      bubbles?: boolean,
      cancelable?: boolean,
      data?: any,
      origin?: string,
      lastEventId?: string,
      source?: object | null,
      ports?: object[],
    ): void;
  }
  var MessageEvent: {
    prototype: MessageEvent;
    new <T>(type: string, eventInitDict?: MessageEventInit<T>): MessageEvent<T>;
  };
  interface CloseEvent extends Event {
    readonly code: number;
    readonly reason: string;
    readonly wasClean: boolean;
  }
  var CloseEvent: {
    prototype: CloseEvent;
    new (type: string, eventInitDict?: CloseEventInit): CloseEvent;
  };
  interface WebSocket extends EventTarget {
    binaryType: BinaryType;
    readonly bufferedAmount: number;
    readonly extensions: string;
    onclose: ((this: WebSocket, ev: CloseEvent) => any) | null;
    onerror: ((this: WebSocket, ev: Event) => any) | null;
    onmessage: ((this: WebSocket, ev: MessageEvent) => any) | null;
    onopen: ((this: WebSocket, ev: Event) => any) | null;
    readonly protocol: string;
    readonly readyState: 0 | 1 | 2 | 3;
    readonly url: string;
    close(code?: number, reason?: string): void;
    send(data: BufferSource | Blob | string): void;
    readonly CONNECTING: 0;
    readonly OPEN: 1;
    readonly CLOSING: 2;
    readonly CLOSED: 3;
    addEventListener<K extends keyof WebSocketEventMap>(
      type: K,
      listener: (this: WebSocket, ev: WebSocketEventMap[K]) => any,
      options?: boolean | AddEventListenerOptions,
    ): void;
    addEventListener(
      type: string,
      listener: EventListenerOrEventListenerObject,
      options?: boolean | AddEventListenerOptions,
    ): void;
    removeEventListener<K extends keyof WebSocketEventMap>(
      type: K,
      listener: (this: WebSocket, ev: WebSocketEventMap[K]) => any,
      options?: boolean | EventListenerOptions,
    ): void;
    removeEventListener(
      type: string,
      listener: EventListenerOrEventListenerObject,
      options?: boolean | EventListenerOptions,
    ): void;
  }
  var WebSocket: {
    prototype: WebSocket;
    new (url: string | URL, protocols?: string | string[]): WebSocket;
    readonly CONNECTING: 0;
    readonly OPEN: 1;
    readonly CLOSING: 2;
    readonly CLOSED: 3;
  };
  interface EventSource extends EventTarget {
    onerror: ((this: EventSource, ev: Event) => any) | null;
    onmessage: ((this: EventSource, ev: MessageEvent) => any) | null;
    onopen: ((this: EventSource, ev: Event) => any) | null;
    readonly readyState: number;
    readonly url: string;
    readonly withCredentials: boolean;
    close(): void;
    readonly CONNECTING: 0;
    readonly OPEN: 1;
    readonly CLOSED: 2;
    addEventListener<K extends keyof EventSourceEventMap>(
      type: K,
      listener: (this: EventSource, ev: EventSourceEventMap[K]) => any,
      options?: boolean | AddEventListenerOptions,
    ): void;
    addEventListener(
      type: string,
      listener: (this: EventSource, event: MessageEvent) => any,
      options?: boolean | AddEventListenerOptions,
    ): void;
    addEventListener(
      type: string,
      listener: EventListenerOrEventListenerObject,
      options?: boolean | AddEventListenerOptions,
    ): void;
    removeEventListener<K extends keyof EventSourceEventMap>(
      type: K,
      listener: (this: EventSource, ev: EventSourceEventMap[K]) => any,
      options?: boolean | EventListenerOptions,
    ): void;
    removeEventListener(
      type: string,
      listener: (this: EventSource, event: MessageEvent) => any,
      options?: boolean | EventListenerOptions,
    ): void;
    removeEventListener(
      type: string,
      listener: EventListenerOrEventListenerObject,
      options?: boolean | EventListenerOptions,
    ): void;
  }
  var EventSource: {
    prototype: EventSource;
    new (url: string | URL, eventSourceInitDict?: EventSourceInit): EventSource;
    readonly CONNECTING: 0;
    readonly OPEN: 1;
    readonly CLOSED: 2;
  };
  type HeadersInit = [string, string][] | Record<string, string> | Headers;
  interface HeadersIterator<T> extends IteratorObject<
    T,
    BuiltinIteratorReturn,
    unknown
  > {
    [Symbol.iterator](): HeadersIterator<T>;
  }
  type BodyInit =
    ReadableStream | Blob | BufferSource | FormData | URLSearchParams | string;
  type RequestInfo = Request | string;
  type ReferrerPolicy =
    | ""
    | "no-referrer"
    | "no-referrer-when-downgrade"
    | "origin"
    | "origin-when-cross-origin"
    | "same-origin"
    | "strict-origin"
    | "strict-origin-when-cross-origin"
    | "unsafe-url";
  type RequestCache =
    | "default"
    | "force-cache"
    | "no-cache"
    | "no-store"
    | "only-if-cached"
    | "reload";
  type RequestCredentials = "include" | "omit" | "same-origin";
  type RequestDestination = "";
  type RequestMode = "cors" | "navigate" | "no-cors" | "same-origin";
  type RequestPriority = "auto" | "high" | "low";
  type RequestRedirect = "error" | "follow" | "manual";
  type ResponseType =
    "basic" | "cors" | "default" | "error" | "opaque" | "opaqueredirect";
  type BinaryType = "arraybuffer" | "blob";
  interface RequestInit {
    body?: BodyInit | null;
    cache?: RequestCache;
    credentials?: RequestCredentials;
    duplex?: "half";
    headers?: HeadersInit;
    integrity?: string;
    keepalive?: boolean;
    method?: string;
    mode?: RequestMode;
    priority?: RequestPriority;
    redirect?: RequestRedirect;
    referrer?: string;
    referrerPolicy?: ReferrerPolicy;
    signal?: AbortSignal | null;
    window?: null;
  }
  interface ResponseInit {
    headers?: HeadersInit;
    status?: number;
    statusText?: string;
  }
  interface MessageEventInit<T = any> extends EventInit {
    data?: T;
    lastEventId?: string;
    origin?: string;
    ports?: object[];
    source?: object | null;
  }
  interface CloseEventInit extends EventInit {
    code?: number;
    reason?: string;
    wasClean?: boolean;
  }
  interface EventSourceInit {
    withCredentials?: boolean;
  }
  interface WebSocketEventMap {
    close: CloseEvent;
    error: Event;
    message: MessageEvent;
    open: Event;
  }
  interface EventSourceEventMap {
    error: Event;
    message: MessageEvent;
    open: Event;
  }
}

interface AbortController extends Web.Fallback<Web.AbortController> {}
declare var AbortController: Web.FallbackValue<
  "AbortController",
  typeof Web.AbortController
>;
interface AbortSignal extends Web.Fallback<Web.AbortSignal> {}
declare var AbortSignal: Web.FallbackValue<
  "AbortSignal",
  typeof Web.AbortSignal
>;
interface Event extends Web.Fallback<Web.Event> {}
declare var Event: Web.FallbackValue<"Event", typeof Web.Event>;
interface EventTarget extends Web.Fallback<Web.EventTarget> {}
declare var EventTarget: Web.FallbackValue<
  "EventTarget",
  typeof Web.EventTarget
>;
interface CustomEvent<T = any> extends Web.Fallback<Web.CustomEvent<T>> {}
declare var CustomEvent: Web.FallbackValue<
  "CustomEvent",
  typeof Web.CustomEvent
>;
interface ErrorEvent extends Web.Fallback<Web.ErrorEvent> {}
declare var ErrorEvent: Web.FallbackValue<"ErrorEvent", typeof Web.ErrorEvent>;
interface DOMException extends Web.Fallback<Web.DOMException> {}
declare var DOMException: Web.FallbackValue<
  "DOMException",
  typeof Web.DOMException
>;
interface QuotaExceededError extends Web.QuotaExceededError {}
declare var QuotaExceededError: typeof Web.QuotaExceededError;
interface TextEncoder extends Web.Fallback<Web.TextEncoder> {}
declare var TextEncoder: Web.FallbackValue<
  "TextEncoder",
  typeof Web.TextEncoder
>;
interface TextDecoder extends Web.Fallback<Web.TextDecoder> {}
declare var TextDecoder: Web.FallbackValue<
  "TextDecoder",
  typeof Web.TextDecoder
>;
interface URL extends Web.Fallback<Web.URL> {}
declare var URL: Web.FallbackValue<"URL", typeof Web.URL>;
interface URLSearchParams extends Web.Fallback<Web.URLSearchParams> {}
declare var URLSearchParams: Web.FallbackValue<
  "URLSearchParams",
  typeof Web.URLSearchParams
>;
interface Crypto extends Web.Fallback<Web.Crypto> {}
declare var Crypto: Web.FallbackValue<"Crypto", typeof Web.Crypto>;
interface CryptoKey extends Web.Fallback<Web.CryptoKey> {}
declare var CryptoKey: Web.FallbackValue<"CryptoKey", typeof Web.CryptoKey>;
interface SubtleCrypto extends Web.Fallback<Web.SubtleCrypto> {}
declare var SubtleCrypto: Web.FallbackValue<
  "SubtleCrypto",
  typeof Web.SubtleCrypto
>;
declare var crypto: Crypto;
interface Blob extends Web.Fallback<Web.Blob> {}
declare var Blob: Web.FallbackValue<"Blob", typeof Web.Blob>;
interface File extends Web.Fallback<Web.File> {}
declare var File: Web.FallbackValue<"File", typeof Web.File>;
interface FormData extends Web.Fallback<Web.FormData> {}
declare var FormData: Web.FallbackValue<"FormData", typeof Web.FormData>;
interface Headers extends Web.Fallback<Web.Headers> {}
declare var Headers: Web.FallbackValue<"Headers", typeof Web.Headers>;
interface Request extends Web.Fallback<Web.Request> {}
declare var Request: Web.FallbackValue<"Request", typeof Web.Request>;
interface Response extends Web.Fallback<Web.Response> {}
declare var Response: Web.FallbackValue<"Response", typeof Web.Response>;
interface RequestInit extends Web.Fallback<Web.RequestInit> {}
interface ResponseInit extends Web.Fallback<Web.ResponseInit> {}
interface EventSourceInit extends Web.Fallback<Web.EventSourceInit> {}
declare function fetch(
  input: string | URL | Request,
  init?: RequestInit,
): Promise<Response>;
interface MessageEvent<T = any> extends Web.Fallback<Web.MessageEvent<T>> {}
declare var MessageEvent: Web.FallbackValue<
  "MessageEvent",
  typeof Web.MessageEvent
>;
interface CloseEvent extends Web.Fallback<Web.CloseEvent> {}
declare var CloseEvent: Web.FallbackValue<"CloseEvent", typeof Web.CloseEvent>;
interface WebSocket extends Web.Fallback<Web.WebSocket> {}
declare var WebSocket: Web.FallbackValue<"WebSocket", typeof Web.WebSocket>;
interface EventSource extends Web.Fallback<Web.EventSource> {}
declare var EventSource: Web.FallbackValue<
  "EventSource",
  typeof Web.EventSource
>;
interface ReadableStream<R = any> extends Web.Fallback<Web.ReadableStream<R>> {}
declare var ReadableStream: Web.FallbackValue<
  "ReadableStream",
  typeof Web.ReadableStream
>;
interface ReadableStreamDefaultReader<R = any> extends Web.Fallback<
  Web.ReadableStreamDefaultReader<R>
> {}
declare var ReadableStreamDefaultReader: Web.FallbackValue<
  "ReadableStreamDefaultReader",
  typeof Web.ReadableStreamDefaultReader
>;
interface ReadableStreamBYOBReader extends Web.Fallback<Web.ReadableStreamBYOBReader> {}
declare var ReadableStreamBYOBReader: Web.FallbackValue<
  "ReadableStreamBYOBReader",
  typeof Web.ReadableStreamBYOBReader
>;
interface ReadableStreamBYOBRequest extends Web.Fallback<Web.ReadableStreamBYOBRequest> {}
declare var ReadableStreamBYOBRequest: Web.FallbackValue<
  "ReadableStreamBYOBRequest",
  typeof Web.ReadableStreamBYOBRequest
>;
interface ReadableStreamDefaultController<R = any> extends Web.Fallback<
  Web.ReadableStreamDefaultController<R>
> {}
declare var ReadableStreamDefaultController: Web.FallbackValue<
  "ReadableStreamDefaultController",
  typeof Web.ReadableStreamDefaultController
>;
interface ReadableByteStreamController extends Web.Fallback<Web.ReadableByteStreamController> {}
declare var ReadableByteStreamController: Web.FallbackValue<
  "ReadableByteStreamController",
  typeof Web.ReadableByteStreamController
>;
interface WritableStream<W = any> extends Web.Fallback<Web.WritableStream<W>> {}
declare var WritableStream: Web.FallbackValue<
  "WritableStream",
  typeof Web.WritableStream
>;
interface WritableStreamDefaultWriter<W = any> extends Web.Fallback<
  Web.WritableStreamDefaultWriter<W>
> {}
declare var WritableStreamDefaultWriter: Web.FallbackValue<
  "WritableStreamDefaultWriter",
  typeof Web.WritableStreamDefaultWriter
>;
interface WritableStreamDefaultController extends Web.Fallback<Web.WritableStreamDefaultController> {}
declare var WritableStreamDefaultController: Web.FallbackValue<
  "WritableStreamDefaultController",
  typeof Web.WritableStreamDefaultController
>;
interface TransformStream<I = any, O = any> extends Web.Fallback<
  Web.TransformStream<I, O>
> {}
declare var TransformStream: Web.FallbackValue<
  "TransformStream",
  typeof Web.TransformStream
>;
interface TransformStreamDefaultController<O = any> extends Web.Fallback<
  Web.TransformStreamDefaultController<O>
> {}
declare var TransformStreamDefaultController: Web.FallbackValue<
  "TransformStreamDefaultController",
  typeof Web.TransformStreamDefaultController
>;
interface ByteLengthQueuingStrategy extends Web.Fallback<Web.ByteLengthQueuingStrategy> {}
declare var ByteLengthQueuingStrategy: Web.FallbackValue<
  "ByteLengthQueuingStrategy",
  typeof Web.ByteLengthQueuingStrategy
>;
interface CountQueuingStrategy extends Web.Fallback<Web.CountQueuingStrategy> {}
declare var CountQueuingStrategy: Web.FallbackValue<
  "CountQueuingStrategy",
  typeof Web.CountQueuingStrategy
>;
interface TextEncoderStream extends Web.Fallback<Web.TextEncoderStream> {}
declare var TextEncoderStream: Web.FallbackValue<
  "TextEncoderStream",
  typeof Web.TextEncoderStream
>;
interface TextDecoderStream extends Web.Fallback<Web.TextDecoderStream> {}
declare var TextDecoderStream: Web.FallbackValue<
  "TextDecoderStream",
  typeof Web.TextDecoderStream
>;
interface CompressionStream extends Web.Fallback<Web.CompressionStream> {}
declare var CompressionStream: Web.FallbackValue<
  "CompressionStream",
  typeof Web.CompressionStream
>;
interface DecompressionStream extends Web.Fallback<Web.DecompressionStream> {}
declare var DecompressionStream: Web.FallbackValue<
  "DecompressionStream",
  typeof Web.DecompressionStream
>;
interface Performance extends Web.Fallback<Web.Performance> {}
declare var performance: Performance;
declare var Performance: Web.FallbackValue<
  "Performance",
  typeof Web.Performance
>;
interface PerformanceEntry extends Web.Fallback<Web.PerformanceEntry> {}
declare var PerformanceEntry: Web.FallbackValue<
  "PerformanceEntry",
  typeof Web.PerformanceEntry
>;
interface PerformanceMark extends Web.Fallback<Web.PerformanceMark> {}
declare var PerformanceMark: Web.FallbackValue<
  "PerformanceMark",
  typeof Web.PerformanceMark
>;
interface PerformanceMeasure extends Web.Fallback<Web.PerformanceMeasure> {}
declare var PerformanceMeasure: Web.FallbackValue<
  "PerformanceMeasure",
  typeof Web.PerformanceMeasure
>;
interface PerformanceObserver extends Web.Fallback<Web.PerformanceObserver> {}
declare var PerformanceObserver: Web.FallbackValue<
  "PerformanceObserver",
  typeof Web.PerformanceObserver
>;
interface PerformanceObserverEntryList extends Web.Fallback<Web.PerformanceObserverEntryList> {}
declare var PerformanceObserverEntryList: Web.FallbackValue<
  "PerformanceObserverEntryList",
  typeof Web.PerformanceObserverEntryList
>;
interface Console extends Web.Fallback<Web.Console> {}
declare var console: Console;
interface WorkerNavigator extends Web.WorkerFallback<Web.WorkerNavigator> {}
declare var WorkerNavigator: Web.WorkerFallbackValue<
  "WorkerNavigator",
  typeof Web.WorkerNavigator
>;
interface Navigator extends Web.Fallback<Web.WorkerNavigator> {}
declare var navigator: Navigator;
declare function atob(data: string): string;
declare function btoa(data: string): string;
interface StructuredSerializeOptions extends Web.Fallback<Web.StructuredSerializeOptions> {}
declare function structuredClone<T = any>(
  value: T,
  options?: StructuredSerializeOptions,
): T;
declare function queueMicrotask(callback: () => void): void;
declare function setTimeout(
  handler: string | ((...args: any[]) => void),
  timeout?: number,
  ...args: any[]
): number;
declare function clearTimeout(id?: number): void;
declare function setInterval(
  handler: string | ((...args: any[]) => void),
  timeout?: number,
  ...args: any[]
): number;
declare function clearInterval(id?: number): void;
