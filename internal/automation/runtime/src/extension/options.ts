import type { CallOptions } from "@swipt/api";

import { invoke } from "./async";

export type Options = CallOptions;

export const check = (
  value: unknown,
  allowed: string[],
): Record<string, unknown> => {
  if (value === undefined) {
    return {};
  }
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new TypeError("expected an options object");
  }
  for (const key of Object.keys(value)) {
    if (key !== "signal" && !allowed.includes(key)) {
      throw new TypeError(
        `unknown option ${JSON.stringify(key)}; the options are ${["signal", ...allowed].join(", ")}`,
      );
    }
  }
  return value as Record<string, unknown>;
};

export const signalOf = (value: unknown): AbortSignal | undefined =>
  (value as Options | undefined)?.signal;

export const optionsOf = (value: unknown, allowed: string[]) => {
  const { signal: _, ...rest } = check(value, allowed);
  return rest;
};

export const point = (value: unknown, name: string): [number, number] => {
  if (
    !Array.isArray(value) ||
    value.length !== 2 ||
    !Array.from(value).every(Number.isFinite)
  ) {
    throw new TypeError(`${name} must be [x, y] of finite numbers`);
  }
  return value as [number, number];
};

export const act = async (
  operation: string,
  args: unknown[],
  options: unknown,
): Promise<void> => {
  await invoke(operation, args, { signal: signalOf(options) });
};
