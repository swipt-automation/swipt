#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import vm from "node:vm";

import { transformAsync } from "@babel/core";
import { build } from "esbuild";

import { AvailabilityChecker } from "./availability";
import { tr } from "./i18n";
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
].join(":");

export function loadProject(root) {
  const manifest = JSON.parse(
    fs.readFileSync(path.join(root, "package.json"), "utf8"),
  );
  const config = manifest.swiplet ?? {};
  if (typeof config.id !== "string" || !idPattern.test(config.id)) {
    throw new Error(
      tr(
        '"swiplet.id" in package.json must be 1 to 128 lowercase letters, digits, dots, underscores and hyphens starting with a letter or a digit',
      ),
    );
  }
  if (typeof config.name !== "string" || config.name === "") {
    throw new Error(
      tr('"swiplet.name" in package.json must be a non-empty string'),
    );
  }
  const localizedNames = config.localizedNames ?? {};
  if (
    localizedNames === null ||
    typeof localizedNames !== "object" ||
    !Object.values(localizedNames).every((name) => typeof name === "string")
  ) {
    throw new Error(
      tr(
        '"swiplet.localizedNames" in package.json must map languages to names',
      ),
    );
  }
  for (const field of ["description", "updateURL"]) {
    if (config[field] !== undefined && typeof config[field] !== "string") {
      throw new Error(
        tr('"{field}" in package.json must be a string', {
          field: `swiplet.${field}`,
        }),
      );
    }
  }
  if (
    config.versionCode !== undefined &&
    (!Number.isInteger(config.versionCode) || config.versionCode < 1)
  ) {
    throw new Error(
      tr('"swiplet.versionCode" in package.json must be a positive integer'),
    );
  }
  const apiManifest = require.resolve("@swipt/api/package.json", {
    paths: [root, import.meta.dirname],
  });
  const {
    apiLevel,
    oldestAPILevel = 1,
    minimumIOSVersion = 27,
  } = JSON.parse(fs.readFileSync(apiManifest, "utf8"));
  if (
    config.minimumAPILevel !== undefined &&
    !(
      Number.isInteger(config.minimumAPILevel) &&
      config.minimumAPILevel >= oldestAPILevel &&
      config.minimumAPILevel <= apiLevel
    )
  ) {
    throw new Error(
      tr(
        '"swiplet.minimumAPILevel" in package.json must be an integer from {oldest} to {level}, the API level of @swipt/api',
        { oldest: oldestAPILevel, level: apiLevel },
      ),
    );
  }
  const version = parseVersion(config.minimumIOSVersion);
  if (
    config.minimumIOSVersion !== undefined &&
    !(
      typeof config.minimumIOSVersion === "number" &&
      version &&
      versionAtLeast(version, parseVersion(minimumIOSVersion))
    )
  ) {
    throw new Error(
      tr(
        '"swiplet.minimumIOSVersion" in package.json must be a version number from {version}, the minimum iOS version of @swipt/api',
        { version: minimumIOSVersion },
      ),
    );
  }
  const entry = (name) =>
    config[name] === undefined ? undefined : path.resolve(root, config[name]);
  return {
    root,
    manifest,
    config,
    localizedNames,
    apiLevel,
    minimumIOSVersion,
    apiDirectory: path.dirname(apiManifest),
    app: entry("app"),
    extension: entry("extension"),
  };
}

const typeImport = (extension) => ({
  name: "swiplet-type-import",
  setup(build) {
    build.onLoad({ filter: /.*/ }, (args) => {
      if (args.path !== extension) {
        return undefined;
      }
      return {
        errors: [
          {
            text: tr(
              "the interface imports {file} at run time; import its type with `import type`",
              { file: path.basename(extension) },
            ),
          },
        ],
      };
    });
  },
});

async function bundle(project) {
  const out = {
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
    out.extension = result.outputFiles.find((file) =>
      file.path.endsWith(".js"),
    ).text;
    const map = JSON.parse(
      result.outputFiles.find((file) => file.path.endsWith(".map")).text,
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
  if (project.app) {
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
    out.app = scoped.code;
  }
  if (out.app === "" && out.extension === "") {
    throw new Error(
      tr('package.json names neither "swiplet.app" nor "swiplet.extension"'),
    );
  }
  return out;
}

const definitionKey = Symbol("definition");

function describeExtension(source) {
  if (source === "") {
    return { service: false, actions: [] };
  }
  const [newer, older] = [true, false].map((result) =>
    evaluateExtension(source, result),
  );
  if (JSON.stringify(newer) !== JSON.stringify(older)) {
    throw new Error(
      tr(
        "the extension defines different actions depending on available(); define the same actions on every system and check available() inside run or an action",
      ),
    );
  }
  return newer;
}

function evaluateExtension(source, availableResult) {
  const loading = (name) => {
    const fail = () => {
      throw new Error(
        tr(
          "{name} is used while extension.js loads; use it inside run or an action",
          { name },
        ),
      );
    };
    const get = (_, key) =>
      key === "__esModule"
        ? true
        : typeof key === "symbol" || key === "then"
          ? undefined
          : fail();
    return new Proxy(fail, { get, apply: fail, construct: fail });
  };
  const modules = {
    "@swipt/api": { available: () => availableResult },
    "@swipt/api/swiplet": {
      defineExtension: (definition) => ({ [definitionKey]: definition }),
      defineAction: (definition) => definition,
      actions: loading("actions"),
      useService: loading("useService"),
    },
    "@swipt/api/file-system": {
      get dataDirectory() {
        throw new Error(
          tr(
            "{name} is used while extension.js loads; use it inside run or an action",
            { name: "dataDirectory" },
          ),
        );
      },
      get cacheDirectory() {
        throw new Error(
          tr(
            "{name} is used while extension.js loads; use it inside run or an action",
            { name: "cacheDirectory" },
          ),
        );
      },
      get assets() {
        throw new Error(
          tr(
            "{name} is used while extension.js loads; use it inside run or an action",
            { name: "assets" },
          ),
        );
      },
    },
    "@swipt/api/accessibility": loading("accessibility"),
    "@swipt/api/input": loading("input"),
    "@swipt/api/device": loading("device"),
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
      preferredLocalizations: (localizations) =>
        localizations.includes("en") ? ["en"] : localizations.slice(0, 1),
    },
    "expo-sqlite": {
      SQLiteDatabase: loading("SQLiteDatabase"),
      SQLiteStatement: loading("SQLiteStatement"),
      SQLiteTaggedQuery: loading("SQLiteTaggedQuery"),
      SQLiteSession: loading("SQLiteSession"),
      openDatabaseSync: () => loading(tr("the database")),
      openDatabaseAsync: () => Promise.resolve(loading(tr("the database"))),
      deserializeDatabaseSync: () => loading(tr("the database")),
      deserializeDatabaseAsync: () =>
        Promise.resolve(loading(tr("the database"))),
      deleteDatabaseSync: () => {},
      deleteDatabaseAsync: () => Promise.resolve(),
      backupDatabaseSync: () => {},
      backupDatabaseAsync: () => Promise.resolve(),
      importDatabaseFromAssetAsync: () => Promise.resolve(),
      addDatabaseChangeListener: () => ({ remove: () => {} }),
      defaultDatabaseDirectory: "",
      bundledExtensions: {},
      configureWeb: () => {},
      deepEqual: function deepEqual(a, b) {
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
          keys.every((key) => deepEqual(a[key], b[key]))
        );
      },
      SQLiteProvider: () => {
        throw new Error(
          tr(
            "SQLiteProvider renders React, so it works only in the swiplet interface",
          ),
        );
      },
      useSQLiteContext: () => {
        throw new Error(
          tr(
            "useSQLiteContext renders React, so it works only in the swiplet interface",
          ),
        );
      },
    },
    "expo-sqlite/kv-store": {
      SQLiteStorage: loading("SQLiteStorage"),
      AsyncStorage: loading(tr("the storage")),
      Storage: loading(tr("the storage")),
      default: loading(tr("the storage")),
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
    ...Object.fromEntries(webGlobals.map((name) => [name, globalThis[name]])),
    fetch: loading("fetch"),
    setTimeout: ignored,
    setInterval: ignored,
    clearTimeout: ignored,
    clearInterval: ignored,
    navigator: { userAgent: "Swipt", language: "en", languages: ["en"] },
  });
  const factory = vm.runInContext(
    `(function (require, module, exports) {${source}\n})`,
    context,
    { filename: "extension.js" },
  );
  const module: { exports: any } = { exports: {} };
  factory(
    (specifier) => {
      if (!Object.hasOwn(modules, specifier)) {
        throw new Error(
          tr(
            'Swipt doesn\'t provide "{module}" to extensions; bundle it into extension.js',
            { module: specifier },
          ),
        );
      }
      return modules[specifier];
    },
    module,
    module.exports,
  );
  const definition = (module.exports.default ?? module.exports)?.[
    definitionKey
  ];
  if (definition === undefined) {
    throw new Error(
      tr("the extension's default export is not the result of defineExtension"),
    );
  }
  const schema = (name, value, side) => {
    if (value === undefined) {
      return undefined;
    }
    const converter = value?.["~standard"]?.jsonSchema;
    if (typeof converter?.[side] !== "function") {
      throw new Error(
        tr(
          "the {side} schema of the action {action} doesn't implement Standard JSON Schema; use a library that does, such as Zod 4",
          { side, action: JSON.stringify(name) },
        ),
      );
    }
    return converter[side]({ target: "draft-2020-12" });
  };
  const actions = Object.entries(definition.actions ?? {}).map(
    ([name, action]: [string, any]) => {
      if (typeof action?.title !== "string" || action.title === "") {
        throw new Error(
          tr("the action {action} needs a title", {
            action: JSON.stringify(name),
          }),
        );
      }
      const expose = action.expose ?? [];
      if (
        !Array.isArray(expose) ||
        !expose.every((caller) => caller === "shortcuts")
      ) {
        throw new Error(
          tr('the action {action} can only expose itself to "shortcuts"', {
            action: JSON.stringify(name),
          }),
        );
      }
      return {
        name,
        title: action.title,
        ...(action.description ? { description: action.description } : {}),
        ...(expose.length > 0 ? { expose } : {}),
        ...(action.input ? { input: schema(name, action.input, "input") } : {}),
        ...(action.output
          ? { output: schema(name, action.output, "output") }
          : {}),
      };
    },
  );
  return { service: typeof definition.run === "function", actions };
}

function swipletManifest(project, output) {
  const { service, actions } = describeExtension(output.extension);
  const { config, localizedNames } = project;
  return {
    id: config.id,
    name: config.name,
    ...(Object.keys(localizedNames).length > 0 ? { localizedNames } : {}),
    ...(config.description ? { description: config.description } : {}),
    ...(project.manifest.version ? { version: project.manifest.version } : {}),
    ...(config.versionCode ? { versionCode: config.versionCode } : {}),
    ...(config.icon ? { icon: config.icon } : {}),
    ...(config.updateURL ? { updateURL: config.updateURL } : {}),
    minimumAPILevel: config.minimumAPILevel ?? project.apiLevel,
    minimumIOSVersion: config.minimumIOSVersion ?? project.minimumIOSVersion,
    ...(output.app ? { interface: true } : {}),
    ...(service ? { service } : {}),
    ...(actions.length > 0 ? { actions } : {}),
  };
}

const sourceFiles = (output) => ({
  ...(output.app ? { "app.js": output.app } : {}),
  ...(output.extension ? { "extension.js": output.extension } : {}),
});

export async function buildProject(root, checker?) {
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
      filter: (source) => !path.basename(source).startsWith("."),
    });
  }
  for (const [name, source] of output.assets) {
    const target = path.join(folder, "assets", name);
    if (fs.existsSync(target)) {
      throw new Error(
        tr("{source} and {asset} would both be assets/{name} in the swiplet", {
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
  execFileSync(
    "zip",
    ["-qr", "-n", storedSuffixes, archive, ...fs.readdirSync(folder)],
    { cwd: folder },
  );
  return { id: manifest.id, archive, digest: folderDigest(folder) };
}

function folderDigest(folder) {
  const hash = createHash("sha256");
  const walk = (directory) => {
    for (const entry of fs
      .readdirSync(directory, { withFileTypes: true })
      .sort((a, b) => (a.name < b.name ? -1 : 1))) {
      const name = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        walk(name);
      } else {
        hash.update(`${path.relative(folder, name)}\0`);
        hash.update(fs.readFileSync(name));
        hash.update("\0");
      }
    }
  };
  walk(folder);
  return hash.digest("hex");
}
