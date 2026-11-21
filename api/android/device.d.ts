/** @available apiLevel 4, android */
import type { CallOptions, ObservableState } from "../index";
import type { ScreenRecording, ScreenStream } from "../index";
import type { DirectoryHandle } from "../file-system";
import type { RasterImage } from "../image";

export type DisplayState =
  "unknown" | "off" | "on" | "doze" | "dozeSuspend" | "vr" | "onSuspend";

export interface DeviceInfo {
  readonly name: string;
  readonly manufacturer: string;
  readonly model: string;
  readonly os: string;
  readonly sdk: number;
  readonly build: string;
  readonly language: string;
}

/** width and height are in pixels, which are the coordinates that input and accessibility take on the display. */
export interface Display {
  /** The ID that the display options take; the default display has 0. */
  readonly id: number;
  readonly type: "internal" | "external" | "virtual" | "overlay" | "wifi";
  readonly name: string;
  readonly width: number;
  readonly height: number;
  readonly densityDpi: number;
  readonly rotation: 0 | 90 | 180 | 270;
  readonly state: DisplayState;
}

export type ScreenStreamOptions = CallOptions & {
  /** In bits per second, for the video that record writes. */
  readonly maximumBitrate?: number;
  readonly display?: number;
};

export declare function info(options?: CallOptions): Promise<DeviceInfo>;
/** The default display comes first. */
export declare const displays: ObservableState<readonly Display[]>;
/**
 * Creates a display that shows only the apps launched on it, and lasts until releaseVirtualDisplay or until Swipt
 * stops. While the device is locked, the keyguard covers it unless Swipt runs as root or the shell holds
 * ADD_ALWAYS_UNLOCKED_DISPLAY.
 */
export declare function createVirtualDisplay(
  options: CallOptions & {
    width: number;
    height: number;
    densityDpi: number;
    name?: string;
  },
): Promise<Display>;
/** Rejects with NotAllowedError for a display that createVirtualDisplay did not create. */
export declare function releaseVirtualDisplay(
  id: number,
  options?: CallOptions,
): Promise<void>;
export declare function screenshot(
  options?: CallOptions & { display?: number },
): Promise<RasterImage>;
/** The frames of the stream span the display in the coordinates that input takes. record writes the video at most at the largest size that the encoder of the device takes, so the frames of a recording can have a scale below 1. */
export declare function openScreenStream(
  options?: ScreenStreamOptions,
): Promise<ScreenStream>;
export declare function openScreenRecording(
  directory: DirectoryHandle,
  options?: CallOptions,
): Promise<ScreenRecording>;
export declare const clipboard: ObservableState<string | null> & {
  set(text: string | null, options?: CallOptions): Promise<void>;
};
export declare const locked: ObservableState<boolean>;
/**
 * Turns the display on and dismisses the keyguard, entering the unlock pattern, PIN, or password configured in Swipt
 * when the keyguard asks for a credential, and resolves with the device unlocked and its display on. Rejects with
 * InvalidStateError when the keyguard asks for a credential while automatic unlock is disabled, set to None or Swipe,
 * or configured for a different lock type.
 */
export declare function unlock(options?: CallOptions): Promise<void>;
/** Turns the display off, and locks the device unless its screen lock is None. */
export declare function lock(options?: CallOptions): Promise<void>;
