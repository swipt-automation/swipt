import type {
  ApplicationEvent,
  InstalledApplication,
} from "@swipt/api/android/applications";

import { invoke, subscribe } from "../async";
import { handleOfURI } from "../files";
import { check, signalOf } from "../options";
import { act, call, displayOf, paramsOf, state, text } from "./call";

export const installed = (options?: unknown) =>
  call<InstalledApplication[]>(
    "applications.installed",
    paramsOf(options, []),
    options as never,
  );
export const icon = async (packageName: unknown, options?: unknown) =>
  handleOfURI(
    await invoke<string>(
      "applications.icon",
      [text(packageName, "the package name")],
      { signal: signalOf(check(options, [])) },
    ),
  );
export const launch = (packageName: unknown, options?: unknown) => {
  const { data, restart, display } = paramsOf(options, [
    "data",
    "restart",
    "display",
  ]);
  if (data !== undefined && typeof data !== "string") {
    throw new TypeError("data must be a URI string");
  }
  if (restart !== undefined && typeof restart !== "boolean") {
    throw new TypeError("restart must be a boolean");
  }
  return act(
    "applications.launch",
    {
      package: text(packageName, "the package name"),
      data,
      restart,
      display: displayOf(display),
    },
    options as never,
  );
};
export const foreground = state<string>("foreground");
export const events = (options?: unknown) =>
  subscribe<ApplicationEvent>(
    "applications.events",
    [],
    signalOf(check(options, [])),
    (value) => Object.freeze(value as ApplicationEvent),
  );
