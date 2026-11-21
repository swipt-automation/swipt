import { act, check, optionsOf, point, type Options } from "../options";

const button = (name: string) => (options?: Options) =>
  act("input.button", [name], check(options, []));

export const tap = (at: unknown, options?: Options) =>
  act(
    "input.tap",
    [point(at, "the point"), optionsOf(options, ["count"])],
    options,
  );
export const swipe = (from: unknown, to: unknown, options?: Options) =>
  act(
    "input.swipe",
    [point(from, "from"), point(to, "to"), optionsOf(options, ["durationMs"])],
    options,
  );
export const drag = (from: unknown, to: unknown, options?: Options) =>
  act(
    "input.drag",
    [
      point(from, "from"),
      point(to, "to"),
      optionsOf(options, ["durationMs", "pressMs"]),
    ],
    options,
  );
export const pinch = (center: unknown, scale: number, options?: Options) =>
  act(
    "input.pinch",
    [point(center, "the center"), scale, optionsOf(options, ["durationMs"])],
    options,
  );
export const touch = (fingers: unknown, options?: Options) =>
  act("input.touch", [fingers], check(options, []));
export const pressKey = (combination: string, options?: Options) => {
  if (typeof combination !== "string") {
    throw new TypeError("the key combination must be a string");
  }
  return act("input.pressKey", [combination], check(options, []));
};
export const pressHome = button("home");
export const openAppSwitcher = button("appSwitcher");
export const pressPower = button("power");
export const pressVolumeUp = button("volumeUp");
export const pressVolumeDown = button("volumeDown");
