/** @available apiLevel 4, android */
import type { CallOptions, Point } from "../index";

export interface InputOptions extends CallOptions {
  /** The ID of the display that takes the input; 0, the default display, when omitted. */
  readonly display?: number;
}

export interface TapOptions extends InputOptions {
  /** How many taps in a row; 1 when omitted. */
  readonly count?: number;
}

export type KeyCode = `KEYCODE_${string}`;
export type TouchSample = [x: number, y: number, ms: number];

/** Points are in the coordinates of the display: pixels from its top left corner in its current rotation. */
export declare function tap(point: Point, options?: TapOptions): Promise<void>;
export declare function swipe(
  from: Point,
  to: Point,
  options?: InputOptions & { durationMs?: number },
): Promise<void>;
export declare function drag(
  from: Point,
  to: Point,
  options?: InputOptions & { durationMs?: number; pressMs?: number },
): Promise<void>;
export declare function pinch(
  center: Point,
  scale: number,
  options?: InputOptions & { durationMs?: number },
): Promise<void>;
export declare function touch(
  fingers: readonly (readonly TouchSample[])[],
  options?: InputOptions,
): Promise<void>;
/** An array is a key combination: Swipt presses its keys in order, and releases them in reverse. */
export declare function pressKey(
  key: KeyCode | readonly KeyCode[],
  options?: InputOptions,
): Promise<void>;
