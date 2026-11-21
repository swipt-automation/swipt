import type {
  DeviceInfo,
  Display,
  ScreenStreamOptions,
} from "@swipt/api/android/device";

import { stream } from "../async";
import { imageResult, run } from "../image";
import { check, optionsOf, signalOf } from "../options";
import { openStream, type ScreenStream } from "../screen";
import { act, call, displayOf, paramsOf, state } from "./call";

export { openRecording as openScreenRecording } from "../screen";

const sizeOf = (value: unknown, name: string): number => {
  if (!Number.isInteger(value) || (value as number) <= 0) {
    throw new TypeError(`${name} must be a positive integer`);
  }
  return value as number;
};

export const info = (options?: unknown) =>
  call<DeviceInfo>("device.info", paramsOf(options, []), options as never);
export const displays = state<readonly Display[]>("displays");
export const createVirtualDisplay = (options: unknown) => {
  const {
    width,
    height,
    densityDpi,
    name = "Swipt",
  } = paramsOf(options, ["width", "height", "densityDpi", "name"]);
  if (typeof name !== "string" || name === "") {
    throw new TypeError("name must be a non-empty string");
  }
  return call<Display>(
    "device.createVirtualDisplay",
    {
      width: sizeOf(width, "width"),
      height: sizeOf(height, "height"),
      densityDpi: sizeOf(densityDpi, "densityDpi"),
      name,
    },
    options as never,
  );
};
export const releaseVirtualDisplay = (id: unknown, options?: unknown) => {
  if (!Number.isInteger(id) || (id as number) < 0) {
    throw new TypeError("the display must be the ID of a display");
  }
  return act(
    "device.releaseVirtualDisplay",
    { ...paramsOf(options, []), display: id },
    options as never,
  );
};
export const screenshot = (options?: unknown) =>
  imageResult(
    run(
      "screenshot",
      { display: displayOf(optionsOf(options, ["display"]).display) },
      [],
      signalOf(options),
    ),
  );
export const openScreenStream = async (
  options?: ScreenStreamOptions,
): Promise<ScreenStream> => {
  const { maximumBitrate, display } = optionsOf(options, [
    "maximumBitrate",
    "display",
  ]);
  if (
    maximumBitrate !== undefined &&
    (typeof maximumBitrate !== "number" ||
      !Number.isInteger(maximumBitrate) ||
      maximumBitrate <= 0)
  ) {
    throw new RangeError(
      "maximumBitrate must be a positive integer of bits per second",
    );
  }
  return openStream({ maximumBitrate, display: displayOf(display) }, options);
};
export const clipboard = Object.freeze({
  get: (options?: unknown) =>
    call<string | null>(
      "device.clipboard",
      paramsOf(options, []),
      options as never,
    ),
  values: (options?: unknown) =>
    stream<string | null>(
      "device.watchClipboard",
      [],
      signalOf(check(options, [])),
      "latest",
    ),
  set: (text: unknown, options?: unknown) => {
    if (text !== null && typeof text !== "string") {
      throw new TypeError("the text must be a string or null");
    }
    return act(
      "device.setClipboard",
      { ...paramsOf(options, []), text },
      options as never,
    );
  },
});
export const locked = state<boolean>("locked");
export const unlock = (options?: unknown) =>
  act("device.unlock", paramsOf(options, []), options as never);
export const lock = (options?: unknown) =>
  act("device.lock", paramsOf(options, []), options as never);
