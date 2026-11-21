import type { ScreenStreamOptions } from "@swipt/api/ios/screen";

import { optionsOf } from "../options";
import { openStream, type ScreenStream } from "../screen";

export { openRecording } from "../screen";

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
  return openStream({ mode: mode ?? "remoteDesktop", maximumBitrate }, options);
};
