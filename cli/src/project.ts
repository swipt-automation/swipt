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

const { version } = JSON.parse(
  fs.readFileSync(new URL("../package.json", import.meta.url), "utf8"),
);
const require = createRequire(import.meta.url);
const { peerDependencies } = require("@swipt/api/package.json");

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

const idPattern = /^[a-z0-9][a-z0-9._-]{0,127}$/;

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

const manifestEntry = /^(?:[^/]+\/)?manifest\.json$/;

function loadSource(root) {
  const manifest = JSON.parse(
    fs.readFileSync(path.join(root, "package.json"), "utf8"),
  );
  const config = manifest.source ?? {};
  for (const field of ["name", "image", "url"]) {
    if (config[field] !== undefined && typeof config[field] !== "string") {
      throw new Error(
        tr('"{field}" in package.json must be a string', {
          field: `source.${field}`,
        }),
      );
    }
  }
  const localizedNames = config.localizedNames ?? {};
  if (
    localizedNames === null ||
    typeof localizedNames !== "object" ||
    !Object.values(localizedNames).every((name) => typeof name === "string")
  ) {
    throw new Error(
      tr('"source.localizedNames" in package.json must map languages to names'),
    );
  }
  if (
    config.icon !== undefined &&
    (typeof config.icon?.symbol !== "string" ||
      (config.icon.color !== undefined &&
        typeof config.icon.color !== "string"))
  ) {
    throw new Error(
      tr(
        '"source.icon" in package.json must have a symbol and may have a color',
      ),
    );
  }
  let prefix;
  if (config.url !== undefined) {
    try {
      prefix = new URL(
        config.url.endsWith("/") ? config.url : `${config.url}/`,
      );
    } catch {
      throw new Error(
        tr('"source.url" in package.json is not an absolute URL'),
      );
    }
  }
  const workspaces = Array.isArray(manifest.workspaces)
    ? manifest.workspaces
    : (manifest.workspaces?.packages ?? []);
  return { root, config, localizedNames, prefix, workspaces };
}

function workspaceSwiplets(source) {
  const files = [];
  for (const pattern of source.workspaces) {
    for (const directory of fs.globSync(pattern, { cwd: source.root }).sort()) {
      const project = path.join(source.root, directory, "package.json");
      if (!fs.existsSync(project)) {
        continue;
      }
      const id = JSON.parse(fs.readFileSync(project, "utf8")).swiplet?.id;
      if (typeof id !== "string") {
        continue;
      }
      const built = path.join(source.root, directory, "dist", `${id}.swiplet`);
      if (!fs.existsSync(built)) {
        throw new Error(
          tr("{file} is missing; build {directory} first", {
            file: path.relative(source.root, built),
            directory,
          }),
        );
      }
      files.push(built);
    }
  }
  return files;
}

function readSwiplet(file) {
  const entry = execFileSync("unzip", ["-Z1", file], { encoding: "utf8" })
    .split("\n")
    .filter((name) => manifestEntry.test(name))
    .sort((a, b) => a.length - b.length)[0];
  if (entry === undefined) {
    throw new Error(`${file} has no manifest.json`);
  }
  const manifest = JSON.parse(
    execFileSync("unzip", ["-p", file, entry], { encoding: "utf8" }),
  );
  if (typeof manifest.id !== "string" || !idPattern.test(manifest.id)) {
    throw new Error(`${file} has no valid swiplet id in its manifest`);
  }
  return { file, manifest, data: fs.readFileSync(file) };
}

export function sourceCommand(root, given) {
  const source = loadSource(root);
  const files =
    given.length > 0
      ? given.map((file) => path.resolve(file))
      : workspaceSwiplets(source);
  if (files.length === 0) {
    throw new Error(
      tr(
        "package.json lists no workspaces with swiplets; name the swiplets to list",
      ),
    );
  }
  const swiplets = new Map();
  for (const read of files.map(readSwiplet)) {
    const { id } = read.manifest;
    if (swiplets.has(id)) {
      throw new Error(
        tr("{id} is both {first} and {second}", {
          id,
          first: swiplets.get(id).file,
          second: read.file,
        }),
      );
    }
    swiplets.set(id, read);
  }
  const image =
    source.config.image === undefined
      ? undefined
      : {
          name: path.basename(source.config.image),
          data: fs.readFileSync(path.resolve(root, source.config.image)),
        };
  const located = (name) =>
    source.prefix ? new URL(name, source.prefix).href : name;
  const dist = path.join(root, "dist");
  fs.rmSync(dist, { recursive: true, force: true });
  fs.mkdirSync(dist, { recursive: true });
  const entries = [...swiplets.keys()].map((id) => {
    const { manifest, data } = swiplets.get(id);
    const name = `${id}.swiplet`;
    fs.writeFileSync(path.join(dist, name), data);
    return {
      ...manifest,
      url: located(name),
      size: data.length,
      sha256: createHash("sha256").update(data).digest("hex"),
    };
  });
  if (image) {
    fs.writeFileSync(path.join(dist, image.name), image.data);
  }
  const index = {
    ...(source.config.name ? { name: source.config.name } : {}),
    ...(Object.keys(source.localizedNames).length > 0
      ? { localizedNames: source.localizedNames }
      : {}),
    ...(source.config.icon
      ? {
          icon: {
            symbol: source.config.icon.symbol,
            ...(source.config.icon.color
              ? { color: source.config.icon.color }
              : {}),
          },
        }
      : {}),
    ...(image ? { image: located(image.name) } : {}),
    swiplets: entries,
  };
  fs.writeFileSync(
    path.join(dist, "swiplets.json"),
    `${JSON.stringify(index, null, 2)}\n`,
  );
  console.log(path.relative(process.cwd(), path.join(dist, "swiplets.json")));
}

const swiptRange = version.includes("-")
  ? version
  : `>=${version}-0 <=${version}`;

const idOf = (name) =>
  name
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^[._-]+/, "")
    .slice(0, 128)
    .replace(/[._-]+$/, "") || "swiplet";

const templates = {
  "package.json": (name) =>
    `${JSON.stringify(
      {
        name: idOf(name),
        version: "0.1.0",
        private: true,
        swiplet: {
          id: idOf(name),
          name,
          versionCode: 1,
          icon: { color: "blue", symbol: "sparkles" },
          app: "./src/App.tsx",
          extension: "./src/extension.ts",
        },
        scripts: { build: "swipt project build", check: "tsc" },
        dependencies: { zod: "^4.6.7" },
        devDependencies: {
          "@swipt/api": swiptRange,
          "@swipt/cli": swiptRange,
          typescript: "^7.0.2",
          ...Object.fromEntries(
            ["@expo/ui", "@types/react", "react"].map((name) => [
              name,
              peerDependencies[name],
            ]),
          ),
        },
      },
      null,
      2,
    )}\n`,
  "tsconfig.json": () =>
    `${JSON.stringify(
      {
        compilerOptions: {
          target: "ES2022",
          module: "ESNext",
          moduleResolution: "Bundler",
          jsx: "react-jsx",
          lib: ["ES2023", "DOM"],
          strict: true,
          noEmit: true,
          skipLibCheck: true,
        },
      },
      null,
      2,
    )}\n`,
  ".gitignore": () => "node_modules/\ndist/\n",
  "src/extension.ts": () => `import * as device from "@swipt/api/device";
import { defineAction, defineExtension } from "@swipt/api/swiplet";

export default defineExtension({
  actions: {
    frontApp: defineAction({
      title: "Find the App in Front",
      run: (_, { signal }) => device.foregroundApplication.get({ signal }),
    }),
  },
});
`,
  "src/App.tsx":
    () => `import { Button, Form, Host, Section, Text } from "@expo/ui/swift-ui";
import { useState } from "react";
import { actions } from "@swipt/api/swiplet";

import type extension from "./extension";

const { frontApp } = actions<typeof extension>();

export default function App() {
  const [app, setApp] = useState("");
  return (
    <Host style={{ flex: 1 }}>
      <Form>
        <Section>
          <Button label="Find the App in Front" onPress={async () => setApp(await frontApp())} />
          {app === "" ? null : <Text>{app}</Text>}
        </Section>
      </Form>
    </Host>
  );
}
`,
};

const quote = (value) =>
  /^[\w@%+=:,./-]+$/.test(value)
    ? value
    : `'${value.replaceAll("'", "'\\''")}'`;

export function createCommand(directory) {
  const root = path.resolve(directory);
  if (fs.existsSync(root) && fs.readdirSync(root).length > 0) {
    throw new Error(tr("{directory} is not empty", { directory }));
  }
  for (const [file, template] of Object.entries(templates)) {
    fs.mkdirSync(path.dirname(path.join(root, file)), { recursive: true });
    fs.writeFileSync(path.join(root, file), template(path.basename(root)));
  }
  const manager = process.env.npm_config_user_agent?.split("/")[0] ?? "npm";
  console.log(
    `cd ${quote(directory)} && ${manager} install && ${manager} run build`,
  );
}
