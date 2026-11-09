import type { ObservableState } from "@swipt/api";
import type {
  RecordingSession as RecordingSessionInterface,
  ScreenRecording as ScreenRecordingInterface,
  ScreenStream as ScreenStreamInterface,
  ScreenStreamOptions,
} from "@swipt/api/screen";

import { stream } from "./async";
import { DOMException } from "./errors";
import { directoryOf, fileOf, locationOf } from "./files";
import {
  adoptOptional,
  finite,
  graphicsNative,
  imageResult,
  RasterImage,
  run,
  type ImageHandle,
} from "./image";
import { check, optionsOf, signalOf, type Options } from "./options";

interface ScreenRecordingHandle {
  readonly info: string;
  frame(time: number): ImageHandle;
}

const streams = new WeakMap<ScreenStream, object>();
const sessions = new WeakMap<RecordingSession, object>();
const recordings = new WeakMap<ScreenRecording, ScreenRecordingHandle>();

const take = (id: number, what: string): object => {
  const handle = graphicsNative().take(id);
  if (handle === null) {
    throw new DOMException(`the ${what} was released`, "InvalidStateError");
  }
  return handle as object;
};

export class RecordingSession implements RecordingSessionInterface {
  constructor(handle: object) {
    sessions.set(this, handle);
    Object.freeze(this);
  }

  async stop(options?: Options): Promise<void> {
    await run(
      "recording.stop",
      {},
      [sessions.get(this)!],
      signalOf(check(options, [])),
    );
  }

  toString(): string {
    return "RecordingSession";
  }
}

export class ScreenStream implements ScreenStreamInterface {
  declare readonly frames: ObservableState<RasterImage>;

  constructor(handle: object) {
    streams.set(this, handle);
    Object.defineProperty(this, "frames", {
      value: Object.freeze({
        get: (options?: Options) =>
          imageResult(
            run("screen.frame", {}, [handle], signalOf(check(options, []))),
          ),
        values: (options?: Options) =>
          stream<RasterImage>(
            "screen.frames",
            () => [graphicsNative().prepare("screen.frames", "{}", [handle])],
            signalOf(check(options, [])),
            "latest",
            adoptOptional,
          ),
      }),
      enumerable: true,
    });
    Object.freeze(this);
  }

  async record(
    directory: unknown,
    options?: Options,
  ): Promise<RecordingSession> {
    const result = await run<{ session: number }>(
      "screen.record",
      { ...locationOf(directoryOf(directory, "the directory")) },
      [streams.get(this)!],
      signalOf(check(options, [])),
    );
    return new RecordingSession(take(result.session, "recording session"));
  }

  async close(options?: Options): Promise<void> {
    await run(
      "screen.close",
      {},
      [streams.get(this)!],
      signalOf(check(options, [])),
    );
  }

  toString(): string {
    return "ScreenStream";
  }
}

export class ScreenRecording implements ScreenRecordingInterface {
  declare readonly start: number;
  declare readonly duration: number;

  constructor(handle: ScreenRecordingHandle) {
    recordings.set(this, handle);
    const info: { start: number; duration: number } = JSON.parse(handle.info);
    Object.defineProperties(this, {
      start: { value: info.start, enumerable: true },
      duration: { value: info.duration, enumerable: true },
    });
    Object.freeze(this);
  }

  frame(time: number): RasterImage {
    const clamped = Math.min(
      Math.max(finite(time, "the time"), 0),
      this.duration,
    );
    return new RasterImage(recordings.get(this)!.frame(clamped));
  }

  async export(file: unknown, options?: Options): Promise<void> {
    await run(
      "recording.export",
      { ...locationOf(fileOf(file, "the file")) },
      [recordings.get(this)!],
      signalOf(check(options, [])),
    );
  }

  toString(): string {
    return `ScreenRecording of ${Math.round(this.duration)} ms`;
  }
}

export const open = async (
  options?: ScreenStreamOptions,
): Promise<ScreenStream> => {
  const { mode, maximumBitrate } = optionsOf(options, [
    "mode",
    "maximumBitrate",
  ]);
  if (mode !== undefined && mode !== "remoteDesktop" && mode !== "coreDevice") {
    throw new TypeError("mode must be remoteDesktop or coreDevice");
  }
  if (maximumBitrate !== undefined) {
    if (mode === "coreDevice") {
      throw new TypeError("maximumBitrate applies only to remoteDesktop");
    }
    if (
      typeof maximumBitrate !== "number" ||
      !Number.isInteger(maximumBitrate) ||
      maximumBitrate <= 0
    ) {
      throw new RangeError(
        "maximumBitrate must be a positive integer of bits per second",
      );
    }
  }
  const result = await run<{ stream: number }>(
    "screen.open",
    { mode: mode ?? "remoteDesktop", maximumBitrate },
    [],
    signalOf(options),
  );
  return new ScreenStream(take(result.stream, "screen stream"));
};

export const openRecording = async (
  directory: unknown,
  options?: Options,
): Promise<ScreenRecording> => {
  const result = await run<{ recording: number }>(
    "recording.open",
    { ...locationOf(directoryOf(directory, "the directory")) },
    [],
    signalOf(check(options, [])),
  );
  return new ScreenRecording(
    take(result.recording, "recording") as ScreenRecordingHandle,
  );
};
