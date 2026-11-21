import { invoke } from "../async";
import { errorFrom } from "../errors";
import {
  type AccessibilityElement,
  refOf,
  snapshotOf,
  type RawSnapshot,
  type Selector,
} from "./elements";
import { act, check, optionsOf, signalOf, type Options } from "../options";
import { encode } from "../selector";

const depthLimit = 56;

const depthOf = (depth: unknown): number | undefined => {
  if (depth === undefined) {
    return undefined;
  }
  if (typeof depth !== "number" || Number.isNaN(depth)) {
    throw new TypeError("depth must be a number");
  }
  if (Number.isFinite(depth) && !Number.isInteger(depth)) {
    throw new TypeError("depth must be an integer");
  }
  if ((depth as number) < 0 || (depth as number) > depthLimit) {
    throw new RangeError(`depth must be from 0 to ${depthLimit}`);
  }
  return depth as number;
};

const view = (options: unknown) => {
  const {
    scope,
    within,
    full = false,
    depth,
  } = optionsOf(options, ["scope", "within", "full", "depth"]);
  if (
    scope !== undefined &&
    scope !== "frontApp" &&
    scope !== "springBoard" &&
    scope !== "fullScreen"
  ) {
    throw new TypeError("scope must be frontApp, springBoard or fullScreen");
  }
  if (typeof full !== "boolean") {
    throw new TypeError("full must be a boolean");
  }
  if (within === undefined) {
    return { scope: scope ?? "frontApp", full, depth: depthOf(depth) };
  }
  if (scope !== undefined) {
    throw new TypeError("scope and within are exclusive");
  }
  return {
    within: elementOf(within, "within"),
    full,
    depth: depthOf(depth),
  };
};

const selectorsOf = (selector: unknown): Selector[] => {
  const selectors = Array.isArray(selector) ? selector : [selector];
  if (selectors.length === 0) {
    throw new TypeError("find needs a selector");
  }
  return selectors as Selector[];
};

const limitOf = (value: unknown, name: string): number => {
  if (value === undefined) {
    return 0;
  }
  if (value === Infinity) {
    return Infinity;
  }
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0) {
    throw new TypeError(
      `${name} must be a non-negative number of milliseconds, or Infinity`,
    );
  }
  return value;
};

const elementOf = (element: unknown, operation: string): string => {
  const ref = refOf(element);
  if (ref === undefined) {
    throw new TypeError(`${operation} takes an element of a snapshot`);
  }
  return ref;
};

interface Found {
  snapshot: RawSnapshot | null;
  seen: number;
  failure?: string;
}

export const snapshot = async (options?: Options) =>
  snapshotOf(
    await invoke<RawSnapshot>("accessibility.snapshot", [view(options)], {
      signal: signalOf(options),
    }),
  );

export const find = async (
  selector: unknown,
  options?: Options,
): Promise<AccessibilityElement | null> => {
  const selectors = selectorsOf(selector);
  const {
    timeoutMs,
    poll = false,
    pollTimeoutMs,
    depth,
  } = optionsOf(options, ["timeoutMs", "poll", "pollTimeoutMs", "depth"]);
  const read = { depth: depthOf(depth) };
  const timeout = limitOf(timeoutMs, "timeoutMs");
  if (typeof poll !== "boolean") {
    throw new TypeError("poll must be a boolean");
  }
  if (pollTimeoutMs !== undefined && !poll) {
    throw new TypeError("pollTimeoutMs applies with poll");
  }
  const pollLimit =
    pollTimeoutMs === undefined
      ? Infinity
      : limitOf(pollTimeoutMs, "pollTimeoutMs");
  const signal = signalOf(options);
  const encoded = selectors.map((item) => encode(item, refOf).value);
  const started = Date.now();
  const deadline = started + timeout;
  const pollUntil = poll ? started + pollLimit : -Infinity;
  for (;;) {
    const found = await invoke<Found>("accessibility.find", [encoded, read], {
      signal,
    });
    if (found.snapshot !== null) {
      const taken = snapshotOf(found.snapshot);
      for (const item of selectors) {
        const element = taken.findAll(item)[0];
        if (element !== undefined) {
          return element;
        }
      }
    }
    const now = Date.now();
    if (now >= deadline) {
      if (found.failure !== undefined) {
        throw errorFrom(JSON.parse(found.failure));
      }
      return null;
    }
    if (now < pollUntil) {
      continue;
    }
    await invoke(
      "accessibility.changed",
      [found.seen, deadline === Infinity ? null : deadline - now],
      { signal },
    );
  }
};

export const setText = (element: unknown, text: unknown, options?: Options) => {
  const ref = elementOf(element, "accessibility.setText");
  if (typeof text !== "string") {
    throw new TypeError("the text must be a string");
  }
  const { timeoutMs } = optionsOf(options, ["timeoutMs"]);
  const limit = limitOf(timeoutMs, "timeoutMs");
  return act(
    "accessibility.setText",
    [ref, text, limit === Infinity ? null : limit],
    options,
  );
};

export const tap = (element: unknown, options?: Options) => {
  const ref = elementOf(element, "accessibility.tap");
  const { timeoutMs } = optionsOf(options, ["timeoutMs"]);
  const limit = limitOf(timeoutMs, "timeoutMs");
  return act(
    "accessibility.tap",
    [ref, limit === Infinity ? null : limit],
    options,
  );
};

export const perform = (
  element: unknown,
  action: unknown,
  options?: Options,
) => {
  const ref = elementOf(element, "accessibility.perform");
  if (typeof action !== "string") {
    throw new TypeError("the action must be a string");
  }
  return act("accessibility.perform", [ref, action], check(options, []));
};
