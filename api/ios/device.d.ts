/** @available apiLevel 4, iOS */
import type { CallOptions, ObservableState } from "../index";
import type { FileHandle } from "../file-system";
import type { RasterImage } from "../image";

export type DisplayState = "off" | "dimmed" | "inactiveOn" | "activeOn";

export interface DeviceInfo {
  readonly name: string;
  readonly model: string;
  readonly os: string;
  readonly build: string;
  readonly language: string;
  readonly screen: {
    readonly width: number;
    readonly height: number;
    readonly scale: number;
    readonly orientation: "portrait" | "portraitUpsideDown" | "landscape";
  };
}

export interface InstalledApplication {
  readonly bundleId: string;
  readonly name: string;
  readonly version?: string;
  readonly build?: string;
}

export interface ApplicationStateEvent {
  readonly type: "launch" | "foreground" | "background";
  readonly bundleId: string;
  readonly pid: number;
  readonly time: number;
}

export interface SimulatedLocation {
  readonly latitude: number;
  readonly longitude: number;
}

export declare function info(options?: CallOptions): Promise<DeviceInfo>;
export declare function screenshot(options?: CallOptions): Promise<RasterImage>;
export declare const foregroundApplication: ObservableState<string>;
export declare const locked: ObservableState<boolean>;
export declare const displayState: ObservableState<DisplayState>;
export declare function unlock(options?: CallOptions): Promise<void>;

export declare function applicationEvents(
  options?: CallOptions,
): AsyncIterable<ApplicationStateEvent>;
export declare function installedApplications(
  options?: CallOptions,
): Promise<InstalledApplication[]>;
/** Answers a PNG file in cacheDirectory. */
export declare function applicationIcon(
  bundleId: string,
  options?: CallOptions,
): Promise<FileHandle>;
export declare function launchApplication(
  bundleId: string,
  options?: CallOptions & { url?: string; restart?: boolean },
): Promise<void>;

export declare function pasteboard(
  options?: CallOptions,
): Promise<string | null>;
export declare function setPasteboard(
  text: string | null,
  options?: CallOptions,
): Promise<void>;

/**
 * The WGS-84 location Swipt simulates, kept across restarts of Swipt until it is
 * set to null or the swiplet that set it is removed. iOS reports the real location
 * while Swipt is not connected to the device, and has no call that reads the
 * simulated location back, so get answers what Swipt last set.
 */
export declare const simulatedLocation: ObservableState<SimulatedLocation | null> & {
  set(location: SimulatedLocation | null, options?: CallOptions): Promise<void>;
};
