import type * as appIntents from "@swipt/api/ios/app-intents";

import { inspect } from "./inspect";

const styles = ["default", "destructive", "cancel"];
const actionNames = [
  "add",
  "addData",
  "book",
  "buy",
  "call",
  "checkIn",
  "continue",
  "create",
  "do",
  "download",
  "filter",
  "find",
  "get",
  "go",
  "log",
  "open",
  "order",
  "pay",
  "play",
  "playSound",
  "post",
  "request",
  "run",
  "search",
  "send",
  "set",
  "share",
  "start",
  "startNavigation",
  "toggle",
  "turnOff",
  "turnOn",
  "view",
];
const types = [
  "text",
  "integer",
  "number",
  "date",
  "time",
  "dateTime",
  "file",
  "person",
  "placemark",
];

type Options = Record<string, unknown> & { signal?: AbortSignal };

const options = (value: unknown, allowed: string[]): Options => {
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
  return value as Options;
};
const text = (value: unknown, name: string): string => {
  if (typeof value !== "string") {
    throw new TypeError(`${name} must be a string, not ${inspect(value)}`);
  }
  return value;
};
const prompt = (value: unknown) =>
  value === undefined ? undefined : text(value, "the prompt");
const flag = (value: unknown, name: string): boolean => {
  if (typeof value !== "boolean") {
    throw new TypeError(`${name} must be a boolean`);
  }
  return value;
};
const strings = (values: unknown, name: string): string[] => {
  if (
    !Array.isArray(values) ||
    values.length === 0 ||
    !values.every((value) => typeof value === "string")
  ) {
    throw new TypeError(`${name} must be a non-empty array of strings`);
  }
  return values;
};
const range = (min: unknown, max: unknown, integer: boolean) => {
  for (const [name, value] of [
    ["min", min],
    ["max", max],
  ] as const) {
    if (
      value !== undefined &&
      !(integer ? Number.isSafeInteger(value) : Number.isFinite(value))
    ) {
      throw new TypeError(
        `${name} must be ${integer ? "an integer" : "a finite number"}`,
      );
    }
  }
  if (
    min !== undefined &&
    max !== undefined &&
    (min as number) > (max as number)
  ) {
    throw new TypeError("min is greater than max");
  }
};

export const createAppIntents = (
  ask: <T>(
    request: Record<string, unknown>,
    inApp: boolean,
    signal?: AbortSignal,
  ) => Promise<T>,
  takeBytes: (data: string) => Uint8Array,
): typeof appIntents => {
  const requestChoice = async <
    T extends string | { title: string; style?: string },
  >(
    choices: T[],
    message: string,
    value?: unknown,
  ): Promise<T> => {
    const { signal } = options(value, []);
    if (!Array.isArray(choices) || choices.length === 0) {
      throw new TypeError("the choices must be a non-empty array");
    }
    const list = choices.map((choice) => {
      if (typeof choice === "string") {
        return { title: choice, style: "default" };
      }
      const { title, style = "default" } = options(choice, ["title", "style"]);
      if (!styles.includes(style as string)) {
        throw new TypeError(`style must be one of ${styles.join(", ")}`);
      }
      return { title: text(title, "a choice's title"), style };
    });
    return choices[
      await ask<number>(
        {
          kind: "choice",
          prompt: text(message, "the prompt"),
          options: list,
        },
        false,
        signal,
      )
    ];
  };
  const requestConfirmation = async (
    message: string,
    value?: unknown,
  ): Promise<void> => {
    const { signal, actionName: name = "continue" } = options(value, [
      "actionName",
    ]);
    let actionName = name;
    if (typeof actionName === "object" && actionName !== null) {
      const {
        accept,
        deny,
        destructive = false,
      } = options(actionName, ["accept", "deny", "destructive"]);
      actionName = {
        accept: text(accept, "accept"),
        deny: text(deny, "deny"),
        destructive: flag(destructive, "destructive"),
      };
    } else if (!actionNames.includes(actionName as string)) {
      throw new TypeError(
        `actionName must be one of ${actionNames.join(", ")}, or an object of accept, deny and destructive`,
      );
    }
    await ask(
      {
        kind: "confirmation",
        prompt: text(message, "the prompt"),
        actionName,
      },
      false,
      signal,
    );
  };
  const requestValue = (async (
    type: string,
    message?: string,
    value?: unknown,
  ): Promise<unknown> => {
    const request = { kind: "value", type, prompt: prompt(message) };
    switch (type) {
      case "text": {
        const {
          signal,
          multiline = false,
          literal = false,
        } = options(value, ["multiline", "literal"]);
        return ask(
          {
            ...request,
            multiline: flag(multiline, "multiline"),
            literal: flag(literal, "literal"),
          },
          true,
          signal,
        );
      }
      case "integer": {
        const {
          signal,
          stepper = false,
          min,
          max,
        } = options(value, ["stepper", "min", "max"]);
        range(min, max, true);
        return ask(
          { ...request, stepper: flag(stepper, "stepper"), min, max },
          true,
          signal,
        );
      }
      case "number": {
        const { signal, min, max } = options(value, ["min", "max"]);
        range(min, max, false);
        return ask({ ...request, min, max }, true, signal);
      }
      case "date":
      case "time":
      case "dateTime":
        return new Date(
          await ask<string>(request, true, options(value, []).signal),
        );
      case "file": {
        const {
          name,
          type: fileType,
          data,
        } = await ask<{ name: string; type: string; data: string }>(
          request,
          true,
          options(value, []).signal,
        );
        return { name, type: fileType, bytes: takeBytes(data) };
      }
      case "person":
      case "placemark":
        return ask(request, true, options(value, []).signal);
    }
    throw new TypeError(`the type must be one of ${types.join(", ")}`);
  }) as typeof appIntents.requestValue;
  const requestDisambiguation = async (
    values: string[],
    message: string,
    value?: unknown,
  ): Promise<string> =>
    values[
      await ask<number>(
        {
          kind: "disambiguation",
          prompt: text(message, "the prompt"),
          values: strings(values, "the values"),
        },
        false,
        options(value, []).signal,
      )
    ];
  const requestValueConfirmation = (
    value: string,
    message: string,
    extra?: unknown,
  ): Promise<boolean> =>
    ask<boolean>(
      {
        kind: "valueConfirmation",
        prompt: text(message, "the prompt"),
        value: text(value, "the value"),
      },
      false,
      options(extra, []).signal,
    );
  return {
    requestChoice,
    requestConfirmation,
    requestValue,
    requestDisambiguation,
    requestValueConfirmation,
  };
};
