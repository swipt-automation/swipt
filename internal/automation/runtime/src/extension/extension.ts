import type * as swiplet from "@swipt/api/swiplet";

import { available } from "./available";
import { chunk } from "./chunks";
import { deviceModules } from "./device";
import { DOMException, failureOf } from "./errors";
import { AbortController } from "./events";
import { assets, cacheDirectory, dataDirectory } from "./files";
import { Client, CookieJar } from "./http";
import { preferredLocalizations } from "./localization";
import { native } from "./native";
import { installLocalStorage, sqlite, sqliteStore } from "./sqlite";

type Caller = swiplet.ActionCaller;
const exposable: readonly Caller[] = ["shortcuts"];

interface StandardSchema {
  "~standard": {
    validate(value: unknown):
      | {
          value?: unknown;
          issues?: readonly {
            message: string;
            path?: readonly (PropertyKey | { key: PropertyKey })[];
          }[];
        }
      | Promise<{
          value?: unknown;
          issues?: readonly {
            message: string;
            path?: readonly (PropertyKey | { key: PropertyKey })[];
          }[];
        }>;
  };
}

interface ActionDefinition {
  title: string;
  description?: string;
  input?: StandardSchema;
  output?: StandardSchema;
  expose?: readonly Caller[];
  run?(input: unknown, context: swiplet.ActionContext): unknown;
  stream?(
    input: unknown,
    context: swiplet.ActionContext,
  ): AsyncIterable<unknown>;
}

interface ExtensionDefinition {
  run?(context: swiplet.RunContext): Promise<void>;
  actions?: Record<string, object>;
}

interface Action {
  readonly input?: StandardSchema;
  readonly output?: StandardSchema;
  readonly run?: (input: unknown, context: swiplet.ActionContext) => unknown;
  readonly stream?: (
    input: unknown,
    context: swiplet.ActionContext,
  ) => AsyncIterable<unknown>;
}

interface Extension {
  run?(context: swiplet.RunContext): Promise<void>;
  actions: Record<string, Action>;
}

const definitions = new WeakMap<object, Extension>();
const actionDefinitions = new WeakMap<object, Action>();

const schemaOf = (value: unknown, name: string): StandardSchema | undefined => {
  if (value === undefined) {
    return undefined;
  }
  if (
    value === null ||
    typeof value !== "object" ||
    typeof (value as StandardSchema)["~standard"]?.validate !== "function"
  ) {
    throw new TypeError(`${name} is not a Standard Schema`);
  }
  return value as StandardSchema;
};

export const defineAction: typeof swiplet.defineAction = (
  definition: ActionDefinition,
) => {
  if (definition === null || typeof definition !== "object") {
    throw new TypeError("the action definition must be an object");
  }
  const { title, description, expose = [], run, stream } = definition;
  if (typeof title !== "string" || title === "") {
    throw new TypeError("an action needs a title");
  }
  if (description !== undefined && typeof description !== "string") {
    throw new TypeError("the description must be a string");
  }
  if (
    !Array.isArray(expose) ||
    !expose.every((caller) => exposable.includes(caller))
  ) {
    throw new TypeError(`expose must be an array of ${exposable.join(", ")}`);
  }
  if ((typeof run === "function") === (typeof stream === "function")) {
    throw new TypeError("an action needs either a run or a stream function");
  }
  if (stream !== undefined && expose.length > 0) {
    throw new TypeError("an action that streams can't be exposed");
  }
  const out = Object.freeze({
    title,
    description,
    expose: Object.freeze([...expose]),
    run,
    stream,
    input: schemaOf(definition.input, "the input"),
    output: schemaOf(definition.output, "the output"),
  });
  actionDefinitions.set(out, out);
  return out as never;
};

export const defineExtension: typeof swiplet.defineExtension = (
  definition: ExtensionDefinition,
) => {
  if (definition === null || typeof definition !== "object") {
    throw new TypeError("the extension definition must be an object");
  }
  if (definition.run !== undefined && typeof definition.run !== "function") {
    throw new TypeError("run must be a function");
  }
  const actions = definition.actions ?? {};
  const resolved: Record<string, Action> = {};
  for (const [name, item] of Object.entries(actions)) {
    const action = actionDefinitions.get(item);
    if (action === undefined) {
      throw new TypeError(
        `the action ${JSON.stringify(name)} was not made with defineAction()`,
      );
    }
    resolved[name] = action;
  }
  const out = Object.freeze({ actions: Object.freeze({ ...actions }) });
  definitions.set(out, { run: definition.run, actions: resolved });
  return out as never;
};

interface RuntimeModules {
  "@swipt/api": typeof import("@swipt/api");
  "@swipt/api/swiplet": typeof import("@swipt/api/swiplet");
  "@swipt/api/file-system": typeof import("@swipt/api/file-system");
  "@swipt/api/image": typeof import("@swipt/api/image");
  "@swipt/api/localization": typeof import("@swipt/api/localization");
  "@swipt/api/http": typeof import("@swipt/api/http");
  "expo-sqlite": typeof import("./types/expo-sqlite");
  "expo-sqlite/kv-store": typeof import("./types/expo-sqlite/kv-store");
  "expo-sqlite/localStorage/install": typeof import("./types/expo-sqlite/localStorage/install");
}

export type Table<Modules> = {
  [Specifier in keyof Modules]: () => Modules[Specifier];
};

const interfaceOnly = (name: string) => (): never => {
  throw new Error(`${name} works only in the swiplet interface`);
};

const runtimeModules = {
  "@swipt/api": () => ({ __esModule: true, available }),
  "@swipt/api/swiplet": () => ({
    __esModule: true,
    defineExtension,
    defineAction,
    actions: interfaceOnly("actions"),
    useService: interfaceOnly("useService"),
    exit: interfaceOnly("exit"),
  }),
  "@swipt/api/file-system": () => ({
    __esModule: true,
    get dataDirectory() {
      return dataDirectory();
    },
    get cacheDirectory() {
      return cacheDirectory();
    },
    get assets() {
      return assets();
    },
  }),
  "@swipt/api/image": () => ({ __esModule: true, ...chunk("api").image }),
  "@swipt/api/localization": () => ({
    __esModule: true,
    preferredLocalizations,
  }),
  "@swipt/api/http": () => ({ __esModule: true, Client, CookieJar }),
  "expo-sqlite": sqlite,
  "expo-sqlite/kv-store": sqliteStore,
  "expo-sqlite/localStorage/install": installLocalStorage,
} satisfies Table<RuntimeModules>;

const renamed: ReadonlyMap<string, string> = new Map(
  ["accessibility", "input", "device", "process", "screen", "app-intents"].map(
    (name) => [`@swipt/api/${name}`, `@swipt/api/ios/${name}`],
  ),
);

export const canonicalSpecifier = (specifier: string): string =>
  renamed.get(specifier) ?? specifier;

export type Declared<Undeclared extends never> = Undeclared;
export type UndeclaredExports<Modules, Tables> = {
  [
    Specifier in keyof Modules & keyof Tables
  ]: Tables[Specifier] extends () => infer Exported
    ? Exclude<keyof Exported, keyof Modules[Specifier] | "__esModule">
    : never;
}[keyof Modules & keyof Tables];
type UndeclaredRuntimeExports = Declared<
  UndeclaredExports<RuntimeModules, typeof runtimeModules>
>;

let bound: Readonly<Record<string, () => object>> | undefined;

export const modules = (): Readonly<Record<string, () => object>> =>
  (bound ??= Object.freeze(
    Object.assign(Object.create(null), runtimeModules, deviceModules),
  ));

const require = (specifier: string): object => {
  const module = modules()[canonicalSpecifier(specifier)];
  if (module === undefined) {
    throw new Error(
      `Swipt doesn't provide "${specifier}" to extensions; bundle it into extension.js`,
    );
  }
  return module();
};

const issuesOf = (
  where: string,
  issues: readonly {
    message: string;
    path?: readonly (PropertyKey | { key: PropertyKey })[];
  }[],
) =>
  new TypeError(
    `the ${where} does not match its schema: ${issues
      .map((issue) => {
        const path = (issue.path ?? [])
          .map((segment) =>
            typeof segment === "object" ? String(segment.key) : String(segment),
          )
          .join(".");
        return path === "" ? issue.message : `${path}: ${issue.message}`;
      })
      .join("; ")}`,
  );

const validate = async (
  schema: StandardSchema | undefined,
  value: unknown,
  where: string,
) => {
  if (schema === undefined) {
    return value;
  }
  const result = await schema["~standard"].validate(value);
  if (result.issues) {
    throw issuesOf(where, result.issues);
  }
  return result.value;
};

let loaded: Extension | undefined;
const controller = new AbortController();

const serialize = (value: unknown): string => {
  let json: string | undefined;
  try {
    json = value === undefined ? "null" : JSON.stringify(value);
  } catch (error) {
    throw new TypeError(
      `the output does not pass through JSON: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  if (json === undefined) {
    throw new TypeError("the output does not pass through JSON");
  }
  return json;
};

const settle = (work: () => string | Promise<string>) => {
  Promise.resolve()
    .then(work)
    .then(
      (json) => native.finish("", json),
      (error) =>
        native.finish(
          failureOf(
            controller.signal.aborted && error === controller.signal.reason
              ? new DOMException("the execution was aborted", "AbortError")
              : error,
          ),
          "",
        ),
    );
};

export const hooks = {
  load(
    factory: (
      require: (specifier: string) => object,
      module: { exports: Record<string, unknown> },
      exports: Record<string, unknown>,
    ) => void,
  ): void {
    const module: { exports: Record<string, unknown> } = { exports: {} };
    factory(require, module, module.exports);
    const exported = (module.exports.default ?? module.exports) as object;
    loaded = definitions.get(exported);
    if (loaded === undefined) {
      throw new TypeError(
        "extension.js must export the result of defineExtension as its default export",
      );
    }
  },
  start(): void {
    const run = loaded?.run;
    if (run === undefined) {
      native.finish(
        JSON.stringify({
          name: "NotSupportedError",
          message: "the extension has no run function",
        }),
        "",
      );
      return;
    }
    settle(async () => {
      await run({ signal: controller.signal });
      return "";
    });
  },
  call(name: string, input: string, caller: Caller): void {
    const definition = Object.hasOwn(loaded?.actions ?? {}, name)
      ? loaded!.actions[name]
      : undefined;
    if (definition === undefined) {
      native.finish(
        JSON.stringify({
          name: "NotFoundError",
          message: `the extension has no action ${JSON.stringify(name)}`,
        }),
        "",
      );
      return;
    }
    const context = Object.freeze({
      signal: controller.signal,
      caller,
    });
    settle(async () => {
      const value = await validate(
        definition.input,
        JSON.parse(input),
        "input",
      );
      const argument =
        definition.input === undefined && value === null ? undefined : value;
      if (definition.stream !== undefined) {
        for await (const item of definition.stream(argument, context)) {
          controller.signal.throwIfAborted();
          native.emit(
            serialize(await validate(definition.output, item, "output")),
          );
        }
        return "";
      }
      return serialize(
        await validate(
          definition.output,
          await definition.run!(argument, context),
          "output",
        ),
      );
    });
  },
  abort(): void {
    controller.abort();
  },
};
