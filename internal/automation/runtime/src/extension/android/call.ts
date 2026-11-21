import type { ObservableState } from "@swipt/api";

import { invoke, stream } from "../async";
import { native } from "../native";
import { check, signalOf, type Options } from "../options";

export const paramsOf = (
  options: unknown,
  allowed: string[],
): Record<string, unknown> => {
  const { signal: _, ...params } = check(options, allowed);
  return Object.fromEntries(
    Object.entries(params).filter(([, value]) => value !== undefined),
  );
};

export const call = <T = void>(
  method: string,
  params: Record<string, unknown>,
  options?: Options,
  data?: Uint8Array,
): Promise<T> =>
  invoke<T>(method, [params, data === undefined ? null : native.put(data)], {
    signal: signalOf(options),
  });

export const act = async (
  method: string,
  params: Record<string, unknown>,
  options?: Options,
  data?: Uint8Array,
): Promise<void> => {
  await call(method, params, options, data);
};

export const callData = async <T = unknown>(
  method: string,
  params: Record<string, unknown>,
  options?: Options,
  data?: Uint8Array,
): Promise<{ value: T; data: Uint8Array }> => {
  const result = await call<{ value: T; data: string }>(
    method,
    params,
    options,
    data,
  );
  return { value: result.value, data: native.take(result.data) };
};

export const text = (value: unknown, name: string): string => {
  if (typeof value !== "string") {
    throw new TypeError(`${name} must be a string`);
  }
  return value;
};

export const bytes = (value: unknown, name: string): Uint8Array => {
  if (typeof value === "string") {
    return new TextEncoder().encode(value);
  }
  if (!(value instanceof Uint8Array)) {
    throw new TypeError(`${name} must be a string or a Uint8Array`);
  }
  return value;
};

export const displayOf = (value: unknown): number | undefined => {
  if (
    value !== undefined &&
    (!Number.isInteger(value) || (value as number) < 0)
  ) {
    throw new TypeError("display must be the ID of a display");
  }
  return value as number | undefined;
};

export const state = <T>(name: string): ObservableState<T> =>
  Object.freeze({
    get: (options?: unknown) =>
      call<T>(
        "device.state",
        { ...paramsOf(options, []), state: name },
        options as never,
      ),
    values: (options?: unknown) =>
      stream<T>(
        "device.watchState",
        [{ ...paramsOf(options, []), state: name }],
        signalOf(options),
        "latest",
      ),
  });
