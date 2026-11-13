import {
  ByteLengthQueuingStrategy,
  CountQueuingStrategy,
  ReadableByteStreamController,
  ReadableStream,
  ReadableStreamBYOBReader,
  ReadableStreamBYOBRequest,
  ReadableStreamDefaultController,
  ReadableStreamDefaultReader,
  TransformStream,
  TransformStreamDefaultController,
  WritableStream,
  WritableStreamDefaultController,
  WritableStreamDefaultWriter,
  internalSlots,
} from "web-streams-polyfill";

import {
  allowSharedBufferSource,
  boolean,
  brand,
  defineInterface,
  dictionary,
  domString,
  enumeration,
  optional,
} from "../shared/webidl";
import { invoke } from "./async";
import { TextDecoder, TextEncoder } from "./encoding";
import { failureOf } from "./errors";
import { native } from "./native";

export {
  ByteLengthQueuingStrategy,
  CountQueuingStrategy,
  ReadableByteStreamController,
  ReadableStream,
  ReadableStreamBYOBReader,
  ReadableStreamBYOBRequest,
  ReadableStreamDefaultController,
  ReadableStreamDefaultReader,
  TransformStream,
  TransformStreamDefaultController,
  WritableStream,
  WritableStreamDefaultController,
  WritableStreamDefaultWriter,
  internalSlots,
};

interface NativeSource {
  id: string | null;
  taken?: string;
  started: boolean;
  finish?: () => void;
  controller?: ReadableByteStreamController;
}

type BufferSource = ArrayBuffer | ArrayBufferView;

declare module "web-streams-polyfill" {
  export function internalSlots(value: object): Record<string, unknown>;
}

const readableFrom = ReadableStream.from;
Object.defineProperty(ReadableStream, "from", {
  ...Object.getOwnPropertyDescriptor(ReadableStream, "from"),
  value: {
    from(this: unknown, asyncIterable: unknown) {
      if (
        (typeof asyncIterable !== "object" &&
          typeof asyncIterable !== "function") ||
        asyncIterable === null
      ) {
        throw new TypeError(
          "ReadableStream.from needs an iterable or async iterable object",
        );
      }
      return Reflect.apply(readableFrom, this, [asyncIterable]);
    },
  }.from,
});

const readableIteratorPrototype: object = Object.getPrototypeOf(
  new ReadableStream().values(),
);
Reflect.deleteProperty(readableIteratorPrototype, Symbol.asyncIterator);
Object.setPrototypeOf(
  readableIteratorPrototype,
  Object.getPrototypeOf(Object.getPrototypeOf(async function* () {}.prototype)),
);
Object.defineProperty(readableIteratorPrototype, Symbol.toStringTag, {
  value: "ReadableStream AsyncIterator",
  configurable: true,
});

const sources = new WeakMap<ReadableStream, NativeSource>();

export const chunkBytes = (chunk: unknown): Uint8Array => {
  if (chunk instanceof Uint8Array) {
    return chunk;
  }
  if (ArrayBuffer.isView(chunk)) {
    return new Uint8Array(chunk.buffer, chunk.byteOffset, chunk.byteLength);
  }
  if (chunk instanceof ArrayBuffer) {
    return new Uint8Array(chunk);
  }
  throw new TypeError("the chunk is not an ArrayBuffer or a view of one");
};

export function nativeReadable(
  id: string,
  finish?: () => void,
): ReadableStream<Uint8Array> {
  const source: NativeSource = { id, started: false, finish };
  const stream = new ReadableStream(
    {
      type: "bytes",
      start(controller) {
        source.controller = controller;
      },
      async pull(controller) {
        source.started = true;
        const current = source.id;
        if (current === null) {
          return;
        }
        let chunk: Uint8Array | null;
        try {
          chunk = await invoke<Uint8Array | null>("stream.read", [current]);
        } catch (error) {
          if (source.id === current) {
            source.id = null;
            source.finish?.();
            controller.error(error);
          }
          return;
        }
        if (source.id !== current) {
          return;
        }
        if (chunk === null) {
          source.id = null;
          source.finish?.();
          controller.close();
          controller.byobRequest?.respond(0);
          return;
        }
        controller.enqueue(chunk);
      },
      cancel(reason) {
        if (source.id !== null) {
          native.release(source.id, failureOf(reason));
          source.id = null;
          source.finish?.();
        }
      },
    },
    { __proto__: null, highWaterMark: 0 } as { highWaterMark: number },
  );
  sources.set(stream, source);
  return stream as ReadableStream<Uint8Array>;
}

export function errorNative(stream: ReadableStream, reason: unknown): void {
  const source = sources.get(stream);
  const id = source?.id ?? source?.taken;
  if (source === undefined || id == null) {
    return;
  }
  native.release(id, failureOf(reason));
  source.id = null;
  source.taken = undefined;
  source.finish?.();
  source.controller?.error(reason);
}

export const disturbed = (stream: ReadableStream): boolean =>
  internalSlots(stream)._disturbed === true;

export function takeNative(stream: ReadableStream): string | null {
  const source = sources.get(stream);
  if (source?.id == null || source.started || stream.locked) {
    return null;
  }
  stream.getReader();
  const id = source.id;
  source.id = null;
  source.taken = id;
  return id;
}

export function nativeWritable(
  id: string,
  accept: (chunk: unknown) => Uint8Array = chunkBytes,
): WritableStream<BufferSource> {
  let open = true;
  return new WritableStream<BufferSource>({
    async write(chunk) {
      let bytes: Uint8Array;
      try {
        bytes = accept(chunk);
      } catch (error) {
        open = false;
        native.release(id, failureOf(error));
        throw error;
      }
      if (bytes.length > 0) {
        try {
          await invoke("stream.write", [id, native.put(bytes)]);
        } catch (error) {
          open = false;
          throw error;
        }
      }
    },
    async close() {
      open = false;
      await invoke("stream.close", [id]);
    },
    abort(reason) {
      if (open) {
        open = false;
        native.release(id, failureOf(reason));
      }
    },
  });
}

export function bytesStream(bytes: Uint8Array): ReadableStream<Uint8Array> {
  return new ReadableStream({
    type: "bytes",
    start(controller) {
      if (bytes.length > 0) {
        controller.enqueue(bytes.slice());
      }
      controller.close();
    },
  }) as ReadableStream<Uint8Array>;
}

export const TextEncoderStream = defineInterface(
  class TextEncoderStream {
    readonly #stream: TransformStream<string, Uint8Array>;

    static [brand](value: object) {
      return #stream in value;
    }

    constructor() {
      const encoder = new TextEncoder();
      let pending = "";
      this.#stream = new TransformStream<string, Uint8Array>({
        transform(chunk, controller) {
          let text = pending + domString(chunk, "the TextEncoderStream chunk");
          pending = "";
          const last = text.charCodeAt(text.length - 1);
          if (last >= 0xd800 && last <= 0xdbff) {
            pending = text.slice(-1);
            text = text.slice(0, -1);
          }
          if (text !== "") {
            controller.enqueue(encoder.encode(text));
          }
        },
        flush(controller) {
          if (pending !== "") {
            controller.enqueue(new Uint8Array([0xef, 0xbf, 0xbd]));
          }
        },
      });
    }

    get encoding(): string {
      return "utf-8";
    }

    get readable(): ReadableStream<Uint8Array> {
      return this.#stream.readable;
    }

    get writable(): WritableStream<string> {
      return this.#stream.writable;
    }
  },
  { construct: [] },
);
export type TextEncoderStream = InstanceType<typeof TextEncoderStream>;

const decoderOptions = dictionary<{ fatal: boolean; ignoreBOM: boolean }>({
  fatal: { convert: boolean, fallback: false },
  ignoreBOM: { convert: boolean, fallback: false },
});

export const TextDecoderStream = defineInterface(
  class TextDecoderStream {
    readonly #decoder: TextDecoder;
    readonly #stream: TransformStream<BufferSource, string>;

    static [brand](value: object) {
      return #decoder in value;
    }

    constructor(
      label = "utf-8",
      options?: { fatal?: boolean; ignoreBOM?: boolean },
    ) {
      const decoder = new TextDecoder(label, options);
      this.#decoder = decoder;
      this.#stream = new TransformStream<BufferSource, string>({
        transform(chunk, controller) {
          const text = decoder.decode(
            allowSharedBufferSource(chunk, "the TextDecoderStream chunk"),
            { stream: true },
          );
          if (text !== "") {
            controller.enqueue(text);
          }
        },
        flush(controller) {
          const text = decoder.decode();
          if (text !== "") {
            controller.enqueue(text);
          }
        },
      });
    }

    get encoding(): string {
      return this.#decoder.encoding;
    }

    get fatal(): boolean {
      return this.#decoder.fatal;
    }

    get ignoreBOM(): boolean {
      return this.#decoder.ignoreBOM;
    }

    get readable(): ReadableStream<string> {
      return this.#stream.readable;
    }

    get writable(): WritableStream<BufferSource> {
      return this.#stream.writable;
    }
  },
  { construct: [optional(domString, "utf-8"), optional(decoderOptions)] },
);
export type TextDecoderStream = InstanceType<typeof TextDecoderStream>;

type CompressionFormat = "brotli" | "gzip" | "deflate" | "deflate-raw";

const compressionFormat = enumeration<CompressionFormat>([
  "brotli",
  "deflate",
  "deflate-raw",
  "gzip",
]);

const compressionPair = (
  format: CompressionFormat,
  mode: "compress" | "decompress",
) => {
  const [writer, reader] = native.compression(format, mode);
  const readable = nativeReadable(reader);
  const accept = (chunk: unknown) => {
    try {
      return chunkBytes(chunk);
    } catch (error) {
      errorNative(readable, error);
      throw error;
    }
  };
  return { readable, writable: nativeWritable(writer, accept) };
};

export const CompressionStream = defineInterface(
  class CompressionStream {
    readonly #pair: {
      readable: ReadableStream<Uint8Array>;
      writable: WritableStream<BufferSource>;
    };

    static [brand](value: object) {
      return #pair in value;
    }

    constructor(format: CompressionFormat) {
      this.#pair = compressionPair(format, "compress");
    }

    get readable(): ReadableStream<Uint8Array> {
      return this.#pair.readable;
    }

    get writable(): WritableStream<BufferSource> {
      return this.#pair.writable;
    }
  },
  { construct: [compressionFormat] },
);
export type CompressionStream = InstanceType<typeof CompressionStream>;

export const DecompressionStream = defineInterface(
  class DecompressionStream {
    readonly #pair: {
      readable: ReadableStream<Uint8Array>;
      writable: WritableStream<BufferSource>;
    };

    static [brand](value: object) {
      return #pair in value;
    }

    constructor(format: CompressionFormat) {
      this.#pair = compressionPair(format, "decompress");
    }

    get readable(): ReadableStream<Uint8Array> {
      return this.#pair.readable;
    }

    get writable(): WritableStream<BufferSource> {
      return this.#pair.writable;
    }
  },
  { construct: [compressionFormat] },
);
export type DecompressionStream = InstanceType<typeof DecompressionStream>;
