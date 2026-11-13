import type { ObservableState } from "@swipt/api";
import type {
  ApplicationStateEvent,
  DeviceInfo,
  DisplayState,
  InstalledApplication,
  SimulatedLocation,
} from "@swipt/api/device";

import { invoke, stream } from "./async";
import { handleOfURI } from "./files";
import { imageResult, run } from "./image";
import { act, check, optionsOf, signalOf, type Options } from "./options";

const state = <T>(name: string): ObservableState<T> =>
  Object.freeze({
    get: (options?: Options) =>
      invoke<T>("device.state", [name], {
        signal: signalOf(check(options, [])),
      }),
    values: (options?: Options) =>
      stream<T>("device.state", [name], signalOf(check(options, [])), "latest"),
  });

const bundleIdOf = (bundleId: unknown): string => {
  if (typeof bundleId !== "string") {
    throw new TypeError("the bundle ID must be a string");
  }
  return bundleId;
};

export const info = (options?: Options) =>
  invoke<DeviceInfo>("device.info", [], {
    signal: signalOf(check(options, [])),
  });
export const screenshot = (options?: Options) =>
  imageResult(run("screenshot", {}, [], signalOf(check(options, []))));
export const foregroundApplication = state<string>("foreground");
export const locked = state<boolean>("locked");
export const displayState = state<DisplayState>("display");
export const simulatedLocation = Object.freeze({
  ...state<SimulatedLocation | null>("simulatedLocation"),
  set: (location: SimulatedLocation | null, options?: Options) => {
    if (location === undefined) {
      throw new TypeError("the location must be an object or null");
    }
    return act("device.setSimulatedLocation", [location], check(options, []));
  },
});
export const unlock = (options?: Options) =>
  act("device.unlock", [], check(options, []));

export const applicationEvents = (options?: Options) =>
  stream<ApplicationStateEvent>(
    "device.applicationEvents",
    [],
    signalOf(check(options, [])),
    "all",
    (value) => Object.freeze(value as ApplicationStateEvent),
  );
export const installedApplications = (options?: Options) =>
  invoke<InstalledApplication[]>("device.installedApplications", [], {
    signal: signalOf(check(options, [])),
  });
export const applicationIcon = async (bundleId: string, options?: Options) =>
  handleOfURI(
    await invoke<string>("device.applicationIcon", [bundleIdOf(bundleId)], {
      signal: signalOf(check(options, [])),
    }),
  );
export const launchApplication = (
  bundleId: string,
  options?: Options & { readonly url?: string; readonly restart?: boolean },
) =>
  act(
    "device.launchApplication",
    [bundleIdOf(bundleId), optionsOf(options, ["url", "restart"])],
    options,
  );

export const pasteboard = (options?: Options) =>
  invoke<string | null>("device.pasteboard", [], {
    signal: signalOf(check(options, [])),
  });
export const setPasteboard = (text: string | null, options?: Options) => {
  if (text !== null && typeof text !== "string") {
    throw new TypeError("the text must be a string or null");
  }
  return act("device.setPasteboard", [text], check(options, []));
};
