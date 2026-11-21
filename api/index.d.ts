import type { DirectoryHandle, FileHandle } from "./file-system";
import type { RasterImage } from "./image";

export interface CallOptions {
  /** Aborting it rejects the pending call, or ends an iteration by throwing, with the reason of the signal. */
  readonly signal?: AbortSignal;
}

export interface ObservableState<T> {
  /** Answers the current value. */
  get(options?: CallOptions): Promise<T>;
  /** Yields the current value, then each change; a value that changes faster than it is read skips to the latest. */
  values(options?: CallOptions): AsyncIterable<T>;
}

export interface ScreenStream {
  /** A frame changes only when the screen does, so values waits after the first frame while the screen stays still. */
  readonly frames: ObservableState<RasterImage>;
  record(
    directory: DirectoryHandle,
    options?: CallOptions,
  ): Promise<RecordingSession>;
  close(options?: CallOptions): Promise<void>;
}

export interface RecordingSession {
  stop(options?: CallOptions): Promise<void>;
}

export interface ScreenRecording {
  /** The Unix time in milliseconds when the recording started. */
  readonly start: number;
  /** In milliseconds. */
  readonly duration: number;
  /** Answers the frame shown time milliseconds after start, which is the time of the frame; a time before 0 or after duration answers the first or the last frame. */
  frame(time: number): RasterImage;
  export(file: FileHandle, options?: CallOptions): Promise<void>;
}

export type Point = [x: number, y: number];
export type Rect = [x: number, y: number, width: number, height: number];
export type Quad = [
  topLeft: Point,
  topRight: Point,
  bottomRight: Point,
  bottomLeft: Point,
];

/** The names of the DOMExceptions that Swipt rejects with; it rejects with TypeError and RangeError for arguments that the declarations rule out, and with Error when Swipt itself fails. */
export type ErrorName =
  | "AbortError"
  | "TimeoutError"
  | "NotFoundError"
  | "NotAllowedError"
  | "NotSupportedError"
  | "InvalidStateError"
  | "UnavailableError"
  | "DataError";

export type JSONValue =
  string | number | boolean | null | JSONValue[] | { [key: string]: JSONValue };

export type Jsonify<T> = T extends string | number | boolean | null
  ? T
  : T extends undefined | symbol | ((...args: any[]) => unknown)
    ? never
    : T extends { toJSON(): infer J }
      ? Jsonify<J>
      : T extends readonly (infer U)[]
        ? Jsonify<U>[]
        : T extends object
          ? { [K in keyof T]: Jsonify<Exclude<T[K], undefined>> }
          : never;

export declare function available(requirements: {
  readonly apiLevel?: number;
  readonly iOS?: true | number;
  readonly android?: true | number;
}): boolean;

declare global {
  function sleep(ms: number, options?: CallOptions): Promise<void>;
}
