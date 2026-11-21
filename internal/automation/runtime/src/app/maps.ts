import { requireNativeModule } from "expo";

import { untilAborted } from "./abort";

const host = requireNativeModule<{
  searchPlaces(query: string): Promise<string>;
}>("SwiptHost");

export const searchPlaces = (
  query: string,
  options: { signal?: AbortSignal } = {},
): Promise<unknown> =>
  untilAborted(options.signal, async () =>
    JSON.parse(await host.searchPlaces(query)),
  );
