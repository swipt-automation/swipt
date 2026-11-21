import { apiLevel } from "@swipt/api/package.json";

import { parseVersion, versionAtLeast } from "../../../../../cli/src/version";
import { native } from "./native";
import { system } from "./system";

const systems: Readonly<
  Record<
    string,
    { parse(value: number): number[] | undefined; expected: string }
  >
> = {
  iOS: { parse: parseVersion, expected: "a non-negative version number" },
  android: {
    parse: (value) =>
      Number.isInteger(value) && value >= 1 ? [value] : undefined,
    expected: "a positive SDK level",
  },
};

const shown = (value: unknown) =>
  typeof value === "string" ? JSON.stringify(value) : String(value);

export function available(requirements: {
  readonly apiLevel?: number;
  readonly iOS?: true | number;
  readonly android?: true | number;
}): boolean {
  if (typeof requirements !== "object" || requirements === null) {
    throw new TypeError("the requirements must be an object");
  }
  let satisfied = true;
  let named = false;
  let onSystem = false;
  for (const [name, value] of Object.entries(requirements)) {
    if (value === undefined) {
      continue;
    }
    if (name === "apiLevel") {
      if (typeof value !== "number" || !Number.isInteger(value) || value < 1) {
        throw new TypeError(
          `the API level must be a positive integer; got ${shown(value)}`,
        );
      }
      satisfied &&= apiLevel >= value;
      continue;
    }
    if (!Object.hasOwn(systems, name)) {
      throw new TypeError(
        `unknown requirement ${JSON.stringify(name)}; the requirements are apiLevel, ${Object.keys(systems).join(", ")}`,
      );
    }
    const { parse, expected } = systems[name];
    const version =
      value === true
        ? []
        : typeof value === "number"
          ? parse(value)
          : undefined;
    if (version === undefined) {
      throw new TypeError(
        `${name} must be true or ${expected}; got ${shown(value)}`,
      );
    }
    named = true;
    onSystem ||=
      name === system &&
      versionAtLeast(
        native.environment().osVersion.split(".").map(Number),
        version,
      );
  }
  return satisfied && (!named || onSystem);
}
