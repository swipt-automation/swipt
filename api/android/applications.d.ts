/** @available apiLevel 4, android */
import type { CallOptions, ObservableState } from "../index";
import type { FileHandle } from "../file-system";

export interface InstalledApplication {
  readonly packageName: string;
  readonly name: string;
  readonly version?: string;
  readonly versionCode: number;
  readonly system?: true;
}

export interface ApplicationEvent {
  /** Android 14 reports no start. */
  readonly type: "start" | "foreground" | "background" | "exit";
  readonly packageName: string;
  readonly pid: number;
  /** The Unix time in milliseconds when Swipt received the event. */
  readonly time: number;
}

export declare function installed(
  options?: CallOptions,
): Promise<InstalledApplication[]>;
/** Answers a PNG file in cacheDirectory. */
export declare function icon(
  packageName: string,
  options?: CallOptions,
): Promise<FileHandle>;
/** Starts the launcher activity of the app, or with data, has the app view that URI. restart stops the app first. */
export declare function launch(
  packageName: string,
  options?: CallOptions & {
    data?: string;
    restart?: boolean;
    display?: number;
  },
): Promise<void>;
/** The app in front on the default display. */
export declare const foreground: ObservableState<string>;
/** Collects the events from the call on, so the events before the iteration begins are kept. */
export declare function events(
  options?: CallOptions,
): AsyncIterable<ApplicationEvent>;
