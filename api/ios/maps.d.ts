/** @available apiLevel 4, iOS */
import type { Placemark } from "./app-intents";

export declare function searchPlaces(
  query: string,
  options?: { signal?: AbortSignal },
): Promise<(Placemark & Required<Pick<Placemark, "coordinate">>)[]>;
