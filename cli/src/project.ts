#!/usr/bin/env node
import { createHash } from "node:crypto";
import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import vm from "node:vm";

import { transformAsync } from "@babel/core";
import { build, type BuildFailure, type Metafile, type Plugin } from "esbuild";
import { type Zippable, zipSync } from "fflate";

import { AvailabilityChecker } from "./availability";
import { t } from "./i18n";
import { materialIcons } from "./material-icons";
import { loadEnvironment, metro } from "./metro";
import { isOS, type OS, type Platform, systems } from "./platform";
import { parseVersion, versionAtLeast } from "./version";

const require = createRequire(import.meta.url);

type ModuleTable = { readonly [Scope in "shared" | OS]: readonly string[] };

const appModules: ModuleTable = {
  shared: [
    "react",
    "react/jsx-runtime",
    "react-native",
    "react-native-worklets",
    "expo",
    "expo-localization",
    "@expo/ui",
    "@expo/ui/community/*",
    "@swipt/api",
    "@swipt/api/swiplet",
    "@swipt/api/file-system",
    "@swipt/api/localization",
    "@swipt/api/http",
    "expo-sqlite",
    "expo-sqlite/kv-store",
    "expo-sqlite/localStorage/install",
  ],
  ios: [
    "expo-maps",
    "@swipt/api/ios/maps",
    "@expo/ui/swift-ui",
    "@expo/ui/swift-ui/modifiers",
    "@swipt/api/swift-ui",
    "@swipt/api/ios/app-intents",
  ],
  android: ["@expo/ui/jetpack-compose", "@expo/ui/jetpack-compose/modifiers"],
};
const extensionModules: ModuleTable = {
  shared: [
    "@swipt/api",
    "@swipt/api/swiplet",
    "@swipt/api/file-system",
    "@swipt/api/image",
    "@swipt/api/localization",
    "@swipt/api/http",
    "expo-sqlite",
    "expo-sqlite/kv-store",
    "expo-sqlite/localStorage/install",
  ],
  ios: [
    "@swipt/api/ios/accessibility",
    "@swipt/api/ios/input",
    "@swipt/api/ios/device",
    "@swipt/api/ios/process",
    "@swipt/api/ios/screen",
    "@swipt/api/ios/app-intents",
  ],
  android: [
    "@swipt/api/android/accessibility",
    "@swipt/api/android/input",
    "@swipt/api/android/applications",
    "@swipt/api/android/notifications",
    "@swipt/api/android/device",
    "@swipt/api/android/shell",
  ],
};

const platformsAPILevel = 4;

export const idPattern = /^[a-z0-9][a-z0-9._-]{0,127}$/;

const storedSuffixes = [
  ".webp",
  ".jxl",
  ".avif",
  ".heic",
  ".heif",
  ".jpg",
  ".jpeg",
  ".png",
  ".gif",
  ".mp4",
  ".mov",
  ".m4a",
  ".mp3",
  ".aac",
  ".zip",
  ".gz",
  ".br",
  ".zst",
  ".xz",
];

const zipEpoch = new Date(1980, 0, 1).getTime();

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const isStringRecord = (value: unknown): value is Record<string, string> =>
  isRecord(value) &&
  Object.values(value).every((name) => typeof name === "string");

export function stringField(
  record: Record<string, unknown>,
  name: string,
  field: string,
) {
  const value = record[name];
  if (value !== undefined && typeof value !== "string") {
    throw new Error(t("project.invalidString", { field }));
  }
  return value;
}

export interface Icon {
  color?: string;
  symbolColor?: string;
  ios?: string;
  android?: string;
}

const iconFields = new Set(["color", "symbolColor", "ios", "android"]);

export function parseIcon(
  value: unknown,
  messages: {
    invalidIcon: string;
    invalidAndroidSymbol: (symbol: string) => string;
  },
): Icon | undefined {
  if (value === undefined) {
    return undefined;
  }
  if (
    !isRecord(value) ||
    !Object.entries(value).every(
      ([name, field]) => iconFields.has(name) && typeof field === "string",
    )
  ) {
    throw new Error(messages.invalidIcon);
  }
  const icon = value as Icon;
  if (icon.android !== undefined && !materialIcons.has(icon.android)) {
    throw new Error(
      messages.invalidAndroidSymbol(JSON.stringify(icon.android)),
    );
  }
  return icon;
}

export function loadProject(root: string) {
  const parsed: unknown = JSON.parse(
    fs.readFileSync(path.join(root, "package.json"), "utf8"),
  );
  const manifest: Record<string, unknown> = isRecord(parsed) ? parsed : {};
  const config = isRecord(manifest.swiplet) ? manifest.swiplet : {};
  if (typeof config.id !== "string" || !idPattern.test(config.id)) {
    throw new Error(t("project.invalidID"));
  }
  if (typeof config.name !== "string" || config.name === "") {
    throw new Error(t("project.invalidName"));
  }
  const localizedNames = config.localizedNames ?? {};
  if (!isStringRecord(localizedNames)) {
    throw new Error(t("project.invalidLocalizedNames"));
  }
  if (
    config.description !== undefined &&
    typeof config.description !== "string"
  ) {
    throw new Error(
      t("project.invalidString", {
        field: "swiplet.description",
      }),
    );
  }
  if (
    config.versionCode !== undefined &&
    (typeof config.versionCode !== "number" ||
      !Number.isInteger(config.versionCode) ||
      config.versionCode < 1)
  ) {
    throw new Error(t("project.invalidVersionCode"));
  }
  const apiManifest = require.resolve("@swipt/api/package.json", {
    paths: [root, import.meta.dirname],
  });
  const api: {
    apiLevel: number;
    oldestAPILevel: number;
    minimumIOSVersion: number;
    minimumAndroidSDK: number;
  } = JSON.parse(fs.readFileSync(apiManifest, "utf8"));
  if (api.apiLevel < platformsAPILevel) {
    throw new Error(
      t("project.outdatedAPI", {
        level: api.apiLevel,
        oldest: platformsAPILevel,
      }),
    );
  }
  const oldestAPILevel = Math.max(api.oldestAPILevel, platformsAPILevel);
  const minimumAPILevel = config.minimumAPILevel ?? api.apiLevel;
  if (
    typeof minimumAPILevel !== "number" ||
    !Number.isInteger(minimumAPILevel) ||
    minimumAPILevel < oldestAPILevel ||
    minimumAPILevel > api.apiLevel
  ) {
    throw new Error(
      t("project.invalidMinimumAPILevel", {
        oldest: oldestAPILevel,
        level: api.apiLevel,
      }),
    );
  }
  const declared = config.platforms;
  if (
    !isRecord(declared) ||
    Object.keys(declared).length === 0 ||
    !Object.keys(declared).every(isOS)
  ) {
    throw new Error(t("project.invalidPlatforms"));
  }
  const platforms: Platform[] = [];
  if (declared.ios !== undefined) {
    if (
      !isRecord(declared.ios) ||
      !Object.keys(declared.ios).every((key) => key === "minimumVersion")
    ) {
      throw new Error(t("project.invalidPlatforms"));
    }
    const raw = declared.ios.minimumVersion;
    const version =
      typeof raw === "number" || typeof raw === "string"
        ? parseVersion(raw)
        : undefined;
    const oldest = parseVersion(api.minimumIOSVersion);
    if (
      version === undefined ||
      oldest === undefined ||
      !versionAtLeast(version, oldest)
    ) {
      throw new Error(
        t("project.invalidMinimumIOSVersion", {
          version: api.minimumIOSVersion,
        }),
      );
    }
    platforms.push({
      os: "ios",
      minimumVersion: version,
    });
  }
  if (declared.android !== undefined) {
    if (
      !isRecord(declared.android) ||
      !Object.keys(declared.android).every((key) => key === "minimumSDK")
    ) {
      throw new Error(t("project.invalidPlatforms"));
    }
    const sdk = declared.android.minimumSDK;
    if (
      typeof sdk !== "number" ||
      !Number.isInteger(sdk) ||
      sdk < api.minimumAndroidSDK
    ) {
      throw new Error(
        t("project.invalidMinimumAndroidSDK", { sdk: api.minimumAndroidSDK }),
      );
    }
    platforms.push({
      os: "android",
      minimumSDK: sdk,
    });
  }
  const icon = parseIcon(config.icon, {
    invalidIcon: t("project.invalidIcon"),
    invalidAndroidSymbol: (symbol) =>
      t("project.invalidAndroidSymbol", { symbol }),
  });
  if (typeof config.app !== "string" || config.app === "") {
    throw new Error(
      t("project.invalidString", {
        field: "swiplet.app",
      }),
    );
  }
  if (
    config.extension !== undefined &&
    (typeof config.extension !== "string" || config.extension === "")
  ) {
    throw new Error(
      t("project.invalidString", {
        field: "swiplet.extension",
      }),
    );
  }
  return {
    root,
    version: manifest.version,
    config: {
      id: config.id,
      name: config.name,
      description: config.description,
      versionCode: config.versionCode,
      icon,
    },
    localizedNames,
    minimumAPILevel,
    platforms,
    apiDirectory: path.dirname(apiManifest),
    app: path.resolve(root, config.app),
    extension:
      config.extension === undefined
        ? undefined
        : path.resolve(root, config.extension),
  };
}

export type Project = ReturnType<typeof loadProject>;

const typeImport = (extension: string): Plugin => ({
  name: "swiplet-type-import",
  setup(build) {
    build.onLoad({ filter: /.*/ }, (args) => {
      if (args.path !== extension) {
        return undefined;
      }
      return {
        errors: [
          {
            text: t("project.interfaceImport", {
              file: path.basename(extension),
            }),
          },
        ],
      };
    });
  },
});

const foreignModules = (
  project: Project,
  os: OS,
  modules: ModuleTable,
): Plugin => ({
  name: "swiplet-foreign-modules",
  setup(build) {
    for (const owner of Object.keys(systems) as OS[]) {
      if (owner === os) {
        continue;
      }
      const text = (module: string) =>
        project.platforms.some((platform) => platform.os === owner)
          ? t("project.foreignModule", {
              module,
              system: systems[owner],
              build: systems[os],
              os: owner,
            })
          : t("project.undeclaredModule", { module, system: systems[owner] });
      for (const module of modules[owner]) {
        const filter = new RegExp(
          `^${module.replace(/[.*+?^${}()|[\]\\/]/g, "\\$&")}$`,
        );
        build.onResolve({ filter }, () => ({
          errors: [{ text: text(module) }],
        }));
      }
    }
  },
});

const resolveExtensions = (os: OS) => [
  `.${os}.tsx`,
  `.${os}.ts`,
  ".native.tsx",
  ".native.ts",
  ".tsx",
  ".ts",
  `.${os}.jsx`,
  `.${os}.js`,
  ".native.js",
  ".jsx",
  ".js",
  ".json",
];

const sourceFolder = (os: OS) => `src/${os}`;

const isBuildFailure = (error: unknown): error is BuildFailure =>
  error instanceof Error && Array.isArray(Reflect.get(error, "errors"));

const entryOf = (metafile: Metafile) => {
  const entry = Object.values(metafile.outputs).find(
    (output) => output.entryPoint !== undefined,
  )?.entryPoint;
  if (entry === undefined) {
    throw new Error("esbuild reported no entry point");
  }
  return path.resolve(entry);
};

export interface Build {
  platform: Platform;
  app: string;
  extension: string;
  extensionMap: string;
  metafiles: Metafile[];
  entries: string[];
}

async function bundle(
  project: Project,
  platform: Platform,
  assets: Map<string, string>,
): Promise<Build> {
  const { os } = platform;
  const out: Build = {
    platform,
    app: "",
    extension: "",
    extensionMap: "",
    metafiles: [],
    entries: [],
  };
  let extensionEntry: string | undefined;
  if (project.extension) {
    const outfile = path.join(
      project.root,
      "dist",
      project.config.id,
      sourceFolder(os),
      "extension.js",
    );
    const result = await build({
      entryPoints: [project.extension],
      outfile,
      bundle: true,
      format: "cjs",
      platform: "neutral",
      mainFields: ["module", "main"],
      resolveExtensions: resolveExtensions(os),
      target: "es2022",
      external: [...extensionModules.shared, ...extensionModules[os]],
      sourcemap: "external",
      sourcesContent: false,
      write: false,
      metafile: true,
      logLevel: "silent",
      plugins: [
        foreignModules(project, os, extensionModules),
        metro(project.root, { platform: os, sourcemap: true }),
      ],
    });
    out.metafiles.push(result.metafile);
    extensionEntry = entryOf(result.metafile);
    out.entries.push(extensionEntry);
    const code = result.outputFiles.find((file) => file.path.endsWith(".js"));
    const sourceMap = result.outputFiles.find((file) =>
      file.path.endsWith(".map"),
    );
    if (code === undefined || sourceMap === undefined) {
      throw new Error("esbuild wrote no extension bundle or source map");
    }
    out.extension = code.text;
    const map: { sources: string[]; sourceRoot?: string } = JSON.parse(
      sourceMap.text,
    );
    const sources = map.sources.map((source) =>
      path
        .relative(
          project.root,
          path.resolve(path.dirname(outfile), map.sourceRoot ?? "", source),
        )
        .split(path.sep)
        .join("/"),
    );
    out.extensionMap = JSON.stringify({
      ...map,
      sourceRoot: undefined,
      sources,
    });
  }
  const result = await build({
    entryPoints: [project.app],
    bundle: true,
    format: "cjs",
    platform: "neutral",
    mainFields: ["react-native", "browser", "module", "main"],
    conditions: ["react-native"],
    resolveExtensions: resolveExtensions(os),
    jsx: "automatic",
    target: "es2020",
    define: { "process.env.NODE_ENV": '"production"', __DEV__: "false" },
    external: [...appModules.shared, ...appModules[os]],
    write: false,
    metafile: true,
    logLevel: "silent",
    plugins: [
      ...(extensionEntry ? [typeImport(extensionEntry)] : []),
      foreignModules(project, os, appModules),
      metro(project.root, { platform: os, assets }),
    ],
  });
  out.metafiles.push(result.metafile);
  out.entries.push(entryOf(result.metafile));
  const scoped = await transformAsync(result.outputFiles[0].text, {
    babelrc: false,
    configFile: false,
    sourceType: "script",
    compact: false,
    plugins: [require.resolve("@babel/plugin-transform-block-scoping")],
  });
  if (typeof scoped?.code !== "string") {
    throw new Error("Babel produced no code for the app bundle");
  }
  out.app = scoped.code;
  return out;
}

const definitionKey = Symbol("definition");

interface ExtensionDescription {
  service: boolean;
  actions: ActionDescription[];
}

interface ActionDescription {
  name: string;
  title: string;
  description?: unknown;
  expose?: "shortcuts"[];
  stream?: true;
  input?: unknown;
  output?: unknown;
}

const member = (value: unknown, key: PropertyKey): unknown =>
  (typeof value === "object" && value !== null) || typeof value === "function"
    ? Reflect.get(value, key)
    : undefined;

const withoutExpose = ({
  service,
  actions,
}: ExtensionDescription): ExtensionDescription => ({
  service,
  actions: actions.map(({ expose: _, ...action }) => action),
});

function evaluateAvailable(
  requirements: unknown,
  project: Project,
  platform: Platform,
  availableResult: boolean,
): boolean {
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
          `the API level must be a positive integer; got ${String(value)}`,
        );
      }
      satisfied &&= project.minimumAPILevel >= value ? true : availableResult;
      continue;
    }
    if (name === "iOS") {
      const version =
        value === true
          ? []
          : typeof value === "number"
            ? parseVersion(value)
            : undefined;
      if (version === undefined) {
        throw new TypeError(
          `iOS must be true or a non-negative version number; got ${String(value)}`,
        );
      }
      named = true;
      onSystem ||=
        platform.os === "ios" &&
        (versionAtLeast(platform.minimumVersion, version)
          ? true
          : availableResult);
      continue;
    }
    if (name === "android") {
      const version =
        value === true
          ? []
          : typeof value === "number" && Number.isInteger(value) && value >= 1
            ? [value]
            : undefined;
      if (version === undefined) {
        throw new TypeError(
          `android must be true or a positive SDK level; got ${String(value)}`,
        );
      }
      named = true;
      onSystem ||=
        platform.os === "android" &&
        (versionAtLeast([platform.minimumSDK], version)
          ? true
          : availableResult);
      continue;
    }
    throw new TypeError(
      `unknown requirement ${JSON.stringify(name)}; the requirements are apiLevel, iOS, android`,
    );
  }
  return satisfied && (!named || onSystem);
}

function describeExtension(
  source: string,
  project: Project,
  platform: Platform,
): ExtensionDescription {
  if (source === "") {
    return { service: false, actions: [] };
  }
  const [newer, older] = [true, false].map((result) =>
    evaluateExtension(source, project, platform, result),
  );
  if (JSON.stringify(newer) !== JSON.stringify(older)) {
    throw new Error(t("project.conditionalActions"));
  }
  return newer;
}

function evaluateExtension(
  source: string,
  project: Project,
  platform: Platform,
  availableResult: boolean,
): ExtensionDescription {
  const failing = (message: string) => {
    const fail = () => {
      throw new Error(message);
    };
    return new Proxy(fail, {
      get: (_, key) =>
        key === "__esModule"
          ? true
          : typeof key === "symbol" || key === "then"
            ? undefined
            : fail(),
      apply: fail,
      construct: fail,
    });
  };
  const loading = (name: string) => failing(t("project.usedAtLoad", { name }));
  const database = () => failing(t("project.databaseAtLoad"));
  const storage = failing(t("project.storageAtLoad"));
  const modules: Record<string, unknown> = {
    "@swipt/api": {
      available: (requirements: unknown) =>
        evaluateAvailable(requirements, project, platform, availableResult),
    },
    "@swipt/api/swiplet": {
      defineExtension: (definition: unknown) => ({
        [definitionKey]: definition,
      }),
      defineAction: (definition: unknown) => definition,
      actions: loading("actions"),
      useService: loading("useService"),
    },
    "@swipt/api/file-system": {
      get dataDirectory() {
        throw new Error(t("project.usedAtLoad", { name: "dataDirectory" }));
      },
      get cacheDirectory() {
        throw new Error(t("project.usedAtLoad", { name: "cacheDirectory" }));
      },
      get assets() {
        throw new Error(t("project.usedAtLoad", { name: "assets" }));
      },
    },
    "@swipt/api/ios/accessibility": loading("accessibility"),
    "@swipt/api/ios/input": loading("input"),
    "@swipt/api/ios/device": loading("device"),
    "@swipt/api/ios/process": loading("process"),
    "@swipt/api/ios/screen": loading("screen"),
    "@swipt/api/ios/app-intents": loading("appIntents"),
    "@swipt/api/android/accessibility": loading("accessibility"),
    "@swipt/api/android/input": loading("input"),
    "@swipt/api/android/applications": loading("applications"),
    "@swipt/api/android/notifications": loading("notifications"),
    "@swipt/api/android/device": loading("device"),
    "@swipt/api/android/shell": loading("shell"),
    "@swipt/api/image": loading("image"),
    "@swipt/api/http": {
      Client: function Client() {
        return loading("Client");
      },
      CookieJar: function CookieJar() {
        return loading("CookieJar");
      },
    },
    "@swipt/api/localization": {
      preferredLocalizations: (localizations: readonly string[]) =>
        localizations.includes("en") ? ["en"] : localizations.slice(0, 1),
    },
    "expo-sqlite": {
      SQLiteDatabase: loading("SQLiteDatabase"),
      SQLiteStatement: loading("SQLiteStatement"),
      SQLiteTaggedQuery: loading("SQLiteTaggedQuery"),
      SQLiteSession: loading("SQLiteSession"),
      openDatabaseSync: database,
      openDatabaseAsync: () => Promise.resolve(database()),
      deserializeDatabaseSync: database,
      deserializeDatabaseAsync: () => Promise.resolve(database()),
      deleteDatabaseSync: () => {},
      deleteDatabaseAsync: () => Promise.resolve(),
      backupDatabaseSync: () => {},
      backupDatabaseAsync: () => Promise.resolve(),
      importDatabaseFromAssetAsync: () => Promise.resolve(),
      addDatabaseChangeListener: () => ({ remove: () => {} }),
      defaultDatabaseDirectory: "",
      bundledExtensions: {},
      configureWeb: () => {},
      deepEqual: function deepEqual(a: unknown, b: unknown): boolean {
        if (a === b) {
          return true;
        }
        if (
          a === null ||
          b === null ||
          typeof a !== "object" ||
          typeof b !== "object"
        ) {
          return false;
        }
        const keys = Object.keys(a);
        return (
          keys.length === Object.keys(b).length &&
          keys.every((key) => deepEqual(member(a, key), member(b, key)))
        );
      },
      SQLiteProvider: () => {
        throw new Error(
          t("project.interfaceOnly", {
            name: "SQLiteProvider",
          }),
        );
      },
      useSQLiteContext: () => {
        throw new Error(
          t("project.interfaceOnly", {
            name: "useSQLiteContext",
          }),
        );
      },
    },
    "expo-sqlite/kv-store": {
      SQLiteStorage: loading("SQLiteStorage"),
      AsyncStorage: storage,
      Storage: storage,
      default: storage,
    },
    get "expo-sqlite/localStorage/install"() {
      context.localStorage = loading("localStorage");
      return {};
    },
  };
  const ignored = () => 0;
  const webGlobals = [
    "MessageEvent",
    "CloseEvent",
    "ErrorEvent",
    "File",
    "FormData",
    "Headers",
    "Request",
    "Response",
    "WebSocket",
    "EventSource",
    "ReadableStream",
    "ReadableStreamDefaultReader",
    "ReadableStreamBYOBReader",
    "ReadableStreamBYOBRequest",
    "ReadableStreamDefaultController",
    "ReadableByteStreamController",
    "WritableStream",
    "WritableStreamDefaultWriter",
    "WritableStreamDefaultController",
    "TransformStream",
    "TransformStreamDefaultController",
    "ByteLengthQueuingStrategy",
    "CountQueuingStrategy",
    "TextEncoderStream",
    "TextDecoderStream",
    "CompressionStream",
    "DecompressionStream",
    "performance",
    "Performance",
    "PerformanceEntry",
    "PerformanceMark",
    "PerformanceMeasure",
    "PerformanceObserver",
    "PerformanceObserverEntryList",
  ];
  const context = vm.createContext({
    console,
    URL,
    URLSearchParams,
    TextEncoder,
    TextDecoder,
    atob,
    btoa,
    structuredClone,
    queueMicrotask,
    crypto,
    AbortController,
    AbortSignal,
    Event,
    EventTarget,
    CustomEvent,
    DOMException,
    Blob,
    ...Object.fromEntries(
      webGlobals.map((name) => [name, Reflect.get(globalThis, name)]),
    ),
    fetch: loading("fetch"),
    setTimeout: ignored,
    setInterval: ignored,
    clearTimeout: ignored,
    clearInterval: ignored,
    navigator: { userAgent: "Swipt", language: "en", languages: ["en"] },
  });
  const factory: (
    require: (specifier: unknown) => unknown,
    module: { exports: unknown },
    exports: unknown,
  ) => void = vm.runInContext(
    `(function (require, module, exports) {${source}\n})`,
    context,
    { filename: "extension.js" },
  );
  const module: { exports: unknown } = { exports: {} };
  factory(
    (specifier) => {
      if (typeof specifier !== "string" || !Object.hasOwn(modules, specifier)) {
        throw new Error(
          t("project.unprovidedModule", { module: String(specifier) }),
        );
      }
      return modules[specifier];
    },
    module,
    module.exports,
  );
  const definition = member(
    member(module.exports, "default") ?? module.exports,
    definitionKey,
  );
  if (definition === undefined) {
    throw new Error(t("project.invalidExtension"));
  }
  const schema = (
    name: string,
    value: unknown,
    side: "input" | "output",
  ): unknown => {
    if (value === undefined) {
      return undefined;
    }
    const converter = member(member(value, "~standard"), "jsonSchema");
    const convert = member(converter, side);
    if (typeof convert !== "function") {
      throw new Error(
        t("project.invalidSchema", { side, action: JSON.stringify(name) }),
      );
    }
    return convert.call(converter, { target: "draft-2020-12" });
  };
  const actions = Object.entries(member(definition, "actions") ?? {}).map(
    ([name, action]: [string, unknown]): ActionDescription => {
      const title = member(action, "title");
      if (typeof title !== "string" || title === "") {
        throw new Error(
          t("project.missingTitle", {
            action: JSON.stringify(name),
          }),
        );
      }
      const expose = member(action, "expose") ?? [];
      if (
        !Array.isArray(expose) ||
        !expose.every(
          (caller: unknown): caller is "shortcuts" => caller === "shortcuts",
        )
      ) {
        throw new Error(
          t("project.invalidExposure", {
            action: JSON.stringify(name),
          }),
        );
      }
      const description = member(action, "description");
      const input = member(action, "input");
      const output = member(action, "output");
      return {
        name,
        title,
        ...(description ? { description } : {}),
        ...(platform.os === "ios" && expose.length > 0 ? { expose } : {}),
        ...(typeof member(action, "stream") === "function"
          ? { stream: true }
          : {}),
        ...(input ? { input: schema(name, input, "input") } : {}),
        ...(output ? { output: schema(name, output, "output") } : {}),
      };
    },
  );
  return {
    service: typeof member(definition, "run") === "function",
    actions,
  };
}

function swipletManifest(project: Project, builds: readonly Build[]) {
  const { config, localizedNames } = project;
  const extensions = new Map<OS, ExtensionDescription>(
    builds.map((output) => [
      output.platform.os,
      describeExtension(output.extension, project, output.platform),
    ]),
  );
  const all = [...extensions.values()];
  const baseline = JSON.stringify(withoutExpose(all[0]));
  if (
    all
      .slice(1)
      .some((entry) => JSON.stringify(withoutExpose(entry)) !== baseline)
  ) {
    throw new Error(t("project.inconsistentExtension"));
  }
  const { service, actions } = extensions.get("ios") ?? all[0];
  return {
    id: config.id,
    name: config.name,
    ...(Object.keys(localizedNames).length > 0 ? { localizedNames } : {}),
    ...(config.description ? { description: config.description } : {}),
    ...(project.version ? { version: project.version } : {}),
    ...(config.versionCode ? { versionCode: config.versionCode } : {}),
    ...(config.icon ? { icon: config.icon } : {}),
    minimumAPILevel: project.minimumAPILevel,
    platforms: Object.fromEntries(
      builds.map(({ platform }) => [
        platform.os,
        platform.os === "ios"
          ? { minimumVersion: platform.minimumVersion.join(".") }
          : { minimumSDK: platform.minimumSDK },
      ]),
    ),
    ...(service ? { service } : {}),
    ...(actions.length > 0 ? { actions } : {}),
  };
}

const sourceFiles = (output: Build) => ({
  "app.js": output.app,
  ...(output.extension
    ? {
        "extension.js": output.extension,
        "extension.js.map": output.extensionMap,
      }
    : {}),
});

export async function buildProject(
  root: string,
  checker?: AvailabilityChecker,
) {
  const project = loadProject(root);
  loadEnvironment(project.root);
  const assets = new Map<string, string>();
  const builds: Build[] = [];
  const failures: string[] = [];
  for (const platform of project.platforms) {
    try {
      builds.push(await bundle(project, platform, assets));
    } catch (error) {
      if (!isBuildFailure(error)) {
        throw error;
      }
      failures.push(
        [
          t("project.buildFailed", { system: systems[platform.os] }),
          ...error.errors.map(({ location, text }) =>
            location === null
              ? `  ${text}`
              : `  ${location.file}:${location.line}:${location.column + 1}: ${text}`,
          ),
        ].join("\n"),
      );
    }
  }
  if (failures.length > 0) {
    throw new Error(failures.join("\n"));
  }
  if (checker === undefined) {
    const owned = new AvailabilityChecker();
    try {
      owned.check(project, builds);
    } finally {
      owned.close();
    }
  } else {
    checker.check(project, builds);
  }
  const manifest = swipletManifest(project, builds);
  const dist = path.join(root, "dist");
  const folder = path.join(dist, manifest.id);
  fs.rmSync(folder, { recursive: true, force: true });
  fs.mkdirSync(folder, { recursive: true });
  fs.writeFileSync(
    path.join(folder, "manifest.json"),
    `${JSON.stringify(manifest, null, 2)}\n`,
  );
  for (const output of builds) {
    const sources = path.join(folder, sourceFolder(output.platform.os));
    fs.mkdirSync(sources, { recursive: true });
    for (const [file, contents] of Object.entries(sourceFiles(output))) {
      fs.writeFileSync(path.join(sources, file), contents);
    }
  }
  if (fs.existsSync(path.join(root, "icon.png"))) {
    fs.copyFileSync(path.join(root, "icon.png"), path.join(folder, "icon.png"));
  }
  if (fs.existsSync(path.join(root, "assets"))) {
    fs.cpSync(path.join(root, "assets"), path.join(folder, "assets"), {
      recursive: true,
      dereference: true,
      filter: (source) => !path.basename(source).startsWith("."),
    });
  }
  for (const [name, source] of assets) {
    const target = path.join(folder, "assets", name);
    if (fs.existsSync(target)) {
      throw new Error(
        t("project.assetConflict", {
          source: path.relative(root, source),
          asset: path.join("assets", name),
          name,
        }),
      );
    }
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.copyFileSync(source, target);
  }
  const archive = path.join(dist, `${manifest.id}.swiplet`);
  fs.rmSync(archive, { force: true });
  fs.rmSync(path.join(dist, `${manifest.id}.zip`), { force: true });
  return { id: manifest.id, archive, digest: writeSwiplet(folder, archive) };
}

function writeSwiplet(folder: string, archive: string) {
  const hash = createHash("sha256");
  const entries: Zippable = {};
  const names = fs
    .readdirSync(folder, { recursive: true, withFileTypes: true })
    .filter((entry) => entry.isFile())
    .map((entry) =>
      path
        .relative(folder, path.join(entry.parentPath, entry.name))
        .split(path.sep)
        .join("/"),
    )
    .sort();
  for (const name of names) {
    const file = path.join(folder, name);
    const data = fs.readFileSync(file);
    const stat = fs.statSync(file);
    hash.update(`${name}\0`);
    hash.update(data);
    hash.update("\0");
    entries[name] = [
      data,
      {
        level: storedSuffixes.some((suffix) =>
          name.toLowerCase().endsWith(suffix),
        )
          ? 0
          : 6,
        mtime: Math.max(stat.mtimeMs, zipEpoch),
        os: 3,
        attrs: ((stat.mode & 0o777) | 0o100000) << 16,
      },
    ];
  }
  fs.writeFileSync(archive, zipSync(entries));
  return hash.digest("hex");
}
