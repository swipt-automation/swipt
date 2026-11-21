/** @available apiLevel 4, iOS */
import type { CallOptions, Point } from "../index";

export interface TapOptions extends CallOptions {
  readonly count?: number;
}

export type KeyboardKey =
  | "return"
  | "backspace"
  | "forwardDelete"
  | "tab"
  | "escape"
  | "space"
  | "left"
  | "right"
  | "up"
  | "down"
  | "home"
  | "end"
  | "pageUp"
  | "pageDown"
  | `f${1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 | 11 | 12}`;
export type KeyboardModifier = "cmd" | "shift" | "ctrl" | "alt";
export type KeyCombination =
  KeyboardKey | `${KeyboardModifier}+${KeyboardKey}` | (string & {});
export type TouchSample = [x: number, y: number, ms: number];

export declare function tap(point: Point, options?: TapOptions): Promise<void>;
export declare function swipe(
  from: Point,
  to: Point,
  options?: CallOptions & { durationMs?: number },
): Promise<void>;
export declare function drag(
  from: Point,
  to: Point,
  options?: CallOptions & { durationMs?: number; pressMs?: number },
): Promise<void>;
export declare function pinch(
  center: Point,
  scale: number,
  options?: CallOptions & { durationMs?: number },
): Promise<void>;
export declare function touch(
  fingers: readonly (readonly TouchSample[])[],
  options?: CallOptions,
): Promise<void>;
export declare function pressKey(
  combination: KeyCombination,
  options?: CallOptions,
): Promise<void>;
export declare function pressHome(options?: CallOptions): Promise<void>;
export declare function openAppSwitcher(options?: CallOptions): Promise<void>;
export declare function pressPower(options?: CallOptions): Promise<void>;
export declare function pressVolumeUp(options?: CallOptions): Promise<void>;
export declare function pressVolumeDown(options?: CallOptions): Promise<void>;
