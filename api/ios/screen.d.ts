/** @available apiLevel 4, iOS */
import type { CallOptions, ScreenRecording, ScreenStream } from "../index";
import type { DirectoryHandle } from "../file-system";

export type ScreenStreamOptions = CallOptions &
  (
    | { readonly mode?: "remoteDesktop"; readonly maximumBitrate?: number }
    | { readonly mode: "coreDevice" }
  );

export declare function open(
  options?: ScreenStreamOptions,
): Promise<ScreenStream>;
export declare function openRecording(
  directory: DirectoryHandle,
  options?: CallOptions,
): Promise<ScreenRecording>;
