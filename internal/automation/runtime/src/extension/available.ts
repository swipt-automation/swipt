import { apiLevel } from "@swipt/api/package.json";

import { parseVersion, versionAtLeast } from "../../../../../cli/src/version";
import { native } from "./native";

const platforms: Readonly<Record<string, string>> = { iOS: "ios" };

const shown = (value: unknown) =>
  typeof value === "string" ? JSON.stringify(value) : String(value);

export function available(requirements: {
  readonly apiLevel?: number;
  readonly iOS?: number;
}): boolean {
  if (typeof requirements !== "object" || requirements === null) {
    throw new TypeError("the requirements must be an object");
  }
  let satisfied = true;
  let platformed = false;
  let onPlatform = false;
  for (const [name, value] of Object.entries(requirements)) {
    if (name === "apiLevel") {
      if (value === undefined) {
        continue;
      }
      if (!Number.isInteger(value) || value < 1) {
        throw new TypeError(
          `the API level must be a positive integer; got ${shown(value)}`,
        );
      }
      satisfied &&= apiLevel >= value;
      continue;
    }
    if (!Object.hasOwn(platforms, name)) {
      throw new TypeError(
        `unknown requirement ${JSON.stringify(name)}; the requirements are apiLevel, ${Object.keys(platforms).join(", ")}`,
      );
    }
    if (value === undefined) {
      continue;
    }
    const version = typeof value === "number" ? parseVersion(value) : undefined;
    if (version === undefined) {
      throw new TypeError(
        `the ${name} version must be a non-negative number; got ${shown(value)}`,
      );
    }
    platformed = true;
    if (platforms[name] === native.os) {
      onPlatform = versionAtLeast(
        native.osVersion.split(".").map(Number),
        version,
      );
    }
  }
  return satisfied && (!platformed || onPlatform);
}
