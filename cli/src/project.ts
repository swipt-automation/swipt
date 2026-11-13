#!/usr/bin/env node
import { createHash } from "node:crypto";
import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import vm from "node:vm";

import { transformAsync } from "@babel/core";
import { build, type Metafile, type Plugin } from "esbuild";
import { type Zippable, zipSync } from "fflate";

import { AvailabilityChecker } from "./availability";
import { t } from "./i18n";
import { loadEnvironment, metro } from "./metro";
import { parseVersion, versionAtLeast } from "./version";

const require = createRequire(import.meta.url);

const appModules = [
  "react",
  "react/jsx-runtime",
  "react-native",
  "react-native-worklets",
  "expo",
  "expo-localization",
  "expo-maps",
  "@expo/ui",
  "@expo/ui/*",
  "@swipt/api",
  "@swipt/api/swiplet",
  "@swipt/api/file-system",
  "@swipt/api/localization",
  "@swipt/api/maps",
  "@swipt/api/http",
  "@swipt/api/app-intents",
  "@swipt/api/swift-ui",
  "expo-sqlite",
  "expo-sqlite/kv-store",
  "expo-sqlite/localStorage/install",
];
const extensionModules = [
  "@swipt/api",
  "@swipt/api/swiplet",
  "@swipt/api/file-system",
  "@swipt/api/accessibility",
  "@swipt/api/input",
  "@swipt/api/device",
  "@swipt/api/process",
  "@swipt/api/screen",
  "@swipt/api/image",
  "@swipt/api/app-intents",
  "@swipt/api/localization",
  "@swipt/api/http",
  "expo-sqlite",
  "expo-sqlite/kv-store",
  "expo-sqlite/localStorage/install",
];

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
  typeof value === "object" && value !== null;

const isStringRecord = (value: unknown): value is Record<string, string> =>
  isRecord(value) &&
  Object.values(value).every((name) => typeof name === "string");

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
  const {
    apiLevel,
    oldestAPILevel = 1,
    minimumIOSVersion = 27,
  }: {
    apiLevel: number;
    oldestAPILevel?: number;
    minimumIOSVersion?: number;
  } = JSON.parse(fs.readFileSync(apiManifest, "utf8"));
  if (
    config.minimumAPILevel !== undefined &&
    !(
      typeof config.minimumAPILevel === "number" &&
      Number.isInteger(config.minimumAPILevel) &&
      config.minimumAPILevel >= oldestAPILevel &&
      config.minimumAPILevel <= apiLevel
    )
  ) {
    throw new Error(
      t("project.invalidMinimumAPILevel", {
        oldest: oldestAPILevel,
        level: apiLevel,
      }),
    );
  }
  const version = parseVersion(config.minimumIOSVersion);
  const oldestVersion = parseVersion(minimumIOSVersion);
  if (
    config.minimumIOSVersion !== undefined &&
    !(
      typeof config.minimumIOSVersion === "number" &&
      version &&
      oldestVersion &&
      versionAtLeast(version, oldestVersion)
    )
  ) {
    throw new Error(
      t("project.invalidMinimumIOSVersion", { version: minimumIOSVersion }),
    );
  }
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
      icon: config.icon,
      minimumAPILevel: config.minimumAPILevel,
      minimumIOSVersion: config.minimumIOSVersion,
    },
    localizedNames,
    apiLevel,
    minimumIOSVersion,
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

interface Bundle {
  app: string;
  extension: string;
  extensionMap: string;
  assets: Map<string, string>;
  metafiles: Metafile[];
}

async function bundle(project: Project) {
  const out: Bundle = {
    app: "",
    extension: "",
    extensionMap: "",
    assets: new Map(),
    metafiles: [],
  };
  loadEnvironment(project.root);
  if (project.extension) {
    const outfile = path.join(
      project.root,
      "dist",
      project.config.id,
      "src",
      "extension.js",
    );
    const result = await build({
      entryPoints: [project.extension],
      outfile,
      bundle: true,
      format: "cjs",
      platform: "neutral",
      mainFields: ["module", "main"],
      target: "es2022",
      external: extensionModules,
      sourcemap: "external",
      sourcesContent: false,
      write: false,
      metafile: true,
      logLevel: "silent",
      plugins: [metro(project.root, { sourcemap: true })],
    });
    out.metafiles.push(result.metafile);
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
    resolveExtensions: [
      ".ios.tsx",
      ".ios.ts",
      ".native.tsx",
      ".native.ts",
      ".tsx",
      ".ts",
      ".ios.jsx",
      ".ios.js",
      ".native.js",
      ".jsx",
      ".js",
      ".json",
    ],
    jsx: "automatic",
    target: "es2020",
    define: { "process.env.NODE_ENV": '"production"', __DEV__: "false" },
    external: appModules,
    write: false,
    metafile: true,
    logLevel: "silent",
    plugins: [
      ...(project.extension ? [typeImport(project.extension)] : []),
      metro(project.root, { assets: out.assets }),
    ],
  });
  out.metafiles.push(result.metafile);
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

function describeExtension(source: string): ExtensionDescription {
  if (source === "") {
    return { service: false, actions: [] };
  }
  const [newer, older] = [true, false].map((result) =>
    evaluateExtension(source, result),
  );
  if (JSON.stringify(newer) !== JSON.stringify(older)) {
    throw new Error(t("project.conditionalActions"));
  }
  return newer;
}

function evaluateExtension(
  source: string,
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
    "@swipt/api": { available: () => availableResult },
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
    "@swipt/api/accessibility": loading("accessibility"),
    "@swipt/api/input": loading("input"),
    "@swipt/api/device": loading("device"),
    "@swipt/api/process": loading("process"),
    "@swipt/api/screen": loading("screen"),
    "@swipt/api/image": loading("image"),
    "@swipt/api/app-intents": loading("appIntents"),
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
        ...(expose.length > 0 ? { expose } : {}),
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

function swipletManifest(project: Project, output: Bundle) {
  const { service, actions } = describeExtension(output.extension);
  const { config, localizedNames } = project;
  return {
    id: config.id,
    name: config.name,
    ...(Object.keys(localizedNames).length > 0 ? { localizedNames } : {}),
    ...(config.description ? { description: config.description } : {}),
    ...(project.version ? { version: project.version } : {}),
    ...(config.versionCode ? { versionCode: config.versionCode } : {}),
    ...(config.icon ? { icon: config.icon } : {}),
    minimumAPILevel: config.minimumAPILevel ?? project.apiLevel,
    minimumIOSVersion: config.minimumIOSVersion ?? project.minimumIOSVersion,
    ...(service ? { service } : {}),
    ...(actions.length > 0 ? { actions } : {}),
  };
}

const sourceFiles = (output: Bundle) => ({
  "app.js": output.app,
  ...(output.extension ? { "extension.js": output.extension } : {}),
});

export async function buildProject(
  root: string,
  checker?: AvailabilityChecker,
) {
  const project = loadProject(root);
  const output = await bundle(project);
  if (checker === undefined) {
    const owned = new AvailabilityChecker();
    try {
      owned.check(project, output.metafiles);
    } finally {
      owned.close();
    }
  } else {
    checker.check(project, output.metafiles);
  }
  const manifest = swipletManifest(project, output);
  const dist = path.join(root, "dist");
  const folder = path.join(dist, manifest.id);
  fs.rmSync(folder, { recursive: true, force: true });
  fs.mkdirSync(path.join(folder, "src"), { recursive: true });
  fs.writeFileSync(
    path.join(folder, "manifest.json"),
    `${JSON.stringify(manifest, null, 2)}\n`,
  );
  for (const [file, contents] of Object.entries(sourceFiles(output))) {
    fs.writeFileSync(path.join(folder, "src", file), contents);
  }
  if (output.extensionMap) {
    fs.writeFileSync(
      path.join(folder, "src", "extension.js.map"),
      output.extensionMap,
    );
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
  for (const [name, source] of output.assets) {
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
