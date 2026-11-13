import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  type NodePath,
  type PluginObj,
  type TransformCaller,
  type TransformOptions,
  template,
  transformAsync,
  types,
} from "@babel/core";
import { loadProjectEnv } from "@expo/env";
import type { Plugin, ResolveResult } from "esbuild";
import { imageSize } from "image-size";

import { tr } from "./i18n";

declare module "@babel/core" {
  interface TransformCaller {
    bundler?: string;
    platform?: string;
    engine?: string;
    isDev?: boolean;
    isServer?: boolean;
    isReactServer?: boolean;
    baseUrl?: string;
    routerRoot?: string;
    projectRoot?: string;
    isHMREnabled?: boolean;
    metroSourceType?: "script" | "module" | "asset";
    isNodeModule?: boolean;
  }
}

const require = createRequire(import.meta.url);
const cliDirectory = path.dirname(fileURLToPath(import.meta.url));

const sourceExtensions = ["js", "jsx", "json", "ts", "tsx", "cjs", "mjs"];
const assetExtensions = [
  "bmp",
  "gif",
  "jpg",
  "jpeg",
  "png",
  "psd",
  "svg",
  "webp",
  "xml",
  "m4v",
  "mov",
  "mp4",
  "mpeg",
  "mpg",
  "webm",
  "aac",
  "aiff",
  "caf",
  "m4a",
  "mp3",
  "wav",
  "html",
  "pdf",
  "yaml",
  "yml",
  "otf",
  "ttf",
  "zip",
  "db",
  "sqlite",
  "sqlite3",
];
const imageExtensions = new Set([
  "png",
  "jpg",
  "jpeg",
  "bmp",
  "gif",
  "webp",
  "psd",
  "svg",
  "tiff",
  "ktx",
]);
const babelConfigNames = [
  ".babelrc",
  ".babelrc.js",
  ".babelrc.cjs",
  ".babelrc.mjs",
  ".babelrc.json",
  ".babelrc.cts",
  "babel.config.js",
  "babel.config.cjs",
  "babel.config.mjs",
  "babel.config.json",
  "babel.config.cts",
  "babel.config.ts",
  "babel.config.mts",
];
const contextModes = ["sync", "eager", "lazy", "lazy-once"];
const scaleSuffixes = ["", "@1x", "@1.5x", "@2x", "@3x", "@4x"];
const platformSuffixes = ["", ".ios", ".native"];
const platformRank = new Map([
  ["ios", 2],
  ["native", 1],
]);

const posix = (file: string) => file.split(path.sep).join("/");
const escape = (text: string) => text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

export function loadEnvironment(root: string) {
  loadProjectEnv(root, { mode: "production", silent: true });
  process.env.BABEL_ENV ??= "production";
}

function defaultPreset(root: string) {
  try {
    return createRequire(path.join(root, "package.json")).resolve(
      "expo/internal/babel-preset",
    );
  } catch {
    return require.resolve("babel-preset-expo");
  }
}

function contextFiles(directory: string, recursive: boolean) {
  if (!fs.existsSync(directory)) {
    return [];
  }
  const extensions = new Set([...sourceExtensions, ...assetExtensions]);
  return fs
    .readdirSync(directory, { encoding: "utf8", recursive })
    .map(posix)
    .filter(
      (file) =>
        extensions.has(path.extname(file).slice(1)) &&
        fs.statSync(path.join(directory, file)).isFile(),
    )
    .sort();
}

function contextArgument<T>(
  call: NodePath<types.CallExpression>,
  index: number,
  fallback: T | undefined,
  expected: string,
  check: (value: unknown) => value is T,
): T {
  if (call.node.arguments[index] === undefined) {
    if (fallback !== undefined) {
      return fallback;
    }
  } else {
    const { confident, value } = call.get("arguments")[index].evaluate();
    if (confident && check(value)) {
      return value;
    }
  }
  throw call.buildCodeFrameError(
    tr("Argument {index} of require.context must be {expected}", {
      index: index + 1,
      expected,
    }),
  );
}

function contextPattern(call: NodePath<types.CallExpression>, index: number) {
  const node = call.node.arguments[index];
  if (node === undefined) {
    return /^\.\/.*$/;
  }
  if (!types.isRegExpLiteral(node)) {
    throw call.buildCodeFrameError(
      tr(
        "Argument {index} of require.context must be a regular expression literal.",
        { index: index + 1 },
      ),
    );
  }
  return new RegExp(node.pattern, node.flags.replace(/[gy]/g, ""));
}

const requireContext: PluginObj = {
  name: "swipt-require-context",
  visitor: {
    CallExpression(call, state) {
      const { callee } = call.node;
      if (
        state.filename === undefined ||
        !types.isMemberExpression(callee) ||
        !types.isIdentifier(callee.object, { name: "require" }) ||
        call.scope.hasBinding("require") ||
        !(callee.computed
          ? types.isStringLiteral(callee.property, { value: "context" })
          : types.isIdentifier(callee.property, { name: "context" }))
      ) {
        return;
      }
      const from = path.dirname(state.filename);
      const directory = path.resolve(
        from,
        contextArgument(
          call,
          0,
          undefined,
          tr("a string"),
          (value) => typeof value === "string",
        ),
      );
      const recursive = contextArgument(
        call,
        1,
        true,
        tr("a Boolean"),
        (value) => typeof value === "boolean",
      );
      const filter = contextPattern(call, 2);
      const mode = contextArgument(
        call,
        3,
        "sync",
        tr("one of {values}", { values: contextModes.join(", ") }),
        (value): value is string =>
          typeof value === "string" && contextModes.includes(value),
      );
      const entries = contextFiles(directory, recursive)
        .map((file) => `./${file}`)
        .filter((key) => filter.test(key))
        .map((key) => {
          const target = posix(path.relative(from, path.join(directory, key)));
          return `${JSON.stringify(key)}: () => require(${JSON.stringify(target.startsWith(".") ? target : `./${target}`)})`;
        });
      call.replaceWith(
        template.expression.ast(`(() => {
        const modules = { ${entries.join(", ")} };
        const context = key => {
          if (!Object.prototype.hasOwnProperty.call(modules, key)) {
            throw Object.assign(new Error("Cannot find module '" + key + "'"), { code: "MODULE_NOT_FOUND" });
          }
          return ${mode === "sync" ? "modules[key]()" : "Promise.resolve().then(modules[key])"};
        };
        context.keys = () => Object.keys(modules);
        context.resolve = key => key;
        context.id = ${JSON.stringify(posix(path.relative(state.cwd, directory)) || ".")};
        return context;
      })()`),
      );
    },
  },
};

const loaderOf = (file: string) => {
  const extension = path.extname(file);
  return extension === ".tsx"
    ? "tsx"
    : [".ts", ".mts", ".cts"].includes(extension)
      ? "ts"
      : "jsx";
};

function archivedAsset(root: string, file: string) {
  const relative = path.relative(root, file).split(path.sep);
  return relative[0] === "assets"
    ? { path: relative.slice(1).join("/"), bundled: false }
    : {
        path: relative.map((part) => (part === ".." ? "__" : part)).join("/"),
        bundled: true,
      };
}

interface AssetVariant {
  scale: number;
  file: string;
}

function assetVariants(resolved: string): AssetVariant[] {
  const directory = path.dirname(resolved);
  const extension = path.extname(resolved);
  const stem = path
    .basename(resolved, extension)
    .replace(/(\.(ios|native))$/, "")
    .replace(/@[\d.]+x$/, "");
  const pattern = new RegExp(
    `^${escape(stem)}(?:@([\\d.]+)x)?(?:\\.(ios|native))?${escape(extension)}$`,
  );
  const scales = new Map<number, { rank: number; file: string }>();
  for (const name of fs.readdirSync(directory)) {
    const match = pattern.exec(name);
    if (match === null) {
      continue;
    }
    const scale = match[1] === undefined ? 1 : Number(match[1]);
    const rank = platformRank.get(match[2]) ?? 0;
    if (Number.isFinite(scale) && (scales.get(scale)?.rank ?? -1) < rank) {
      scales.set(scale, { rank, file: path.join(directory, name) });
    }
  }
  return [...scales]
    .sort(([a], [b]) => a - b)
    .map(([scale, { file }]) => ({ scale, file }));
}

function assetModule(
  root: string,
  variants: AssetVariant[],
  assets: Map<string, string>,
) {
  const [first] = variants;
  const extension = path.extname(first.file).slice(1);
  const size = imageExtensions.has(extension)
    ? imageSize(fs.readFileSync(first.file))
    : undefined;
  const files = variants.map(({ scale, file }) => {
    const archived = archivedAsset(root, file);
    if (archived.bundled) {
      assets.set(archived.path, file);
    }
    return [scale, archived.path];
  });
  return `const { PixelRatio } = require("react-native");
const { assets } = require("@swipt/api/file-system");
const files = ${JSON.stringify(files)};
const ratio = PixelRatio.get();
const [scale, file] = files.find(([scale]) => scale >= ratio) ?? files[files.length - 1];
module.exports = { uri: assets.file(file).uri, scale${size ? `, width: ${size.width / first.scale}, height: ${size.height / first.scale}` : ""} };
`;
}

function checkWorklets(root: string) {
  let plugin;
  try {
    plugin = require.resolve("react-native-worklets/plugin", { paths: [root] });
  } catch {
    return;
  }
  const { version } = JSON.parse(
    fs.readFileSync(
      path.join(path.dirname(plugin), "..", "package.json"),
      "utf8",
    ),
  );
  const { version: expected } = require("react-native-worklets/package.json");
  if (version !== expected) {
    throw new Error(
      tr(
        "The project has react-native-worklets {version}, but Swipt runs {expected}; install react-native-worklets@{expected} or remove it.",
        { version, expected },
      ),
    );
  }
}

interface MetroOptions {
  assets?: Map<string, string>;
  sourcemap?: boolean;
}

export function metro(
  root: string,
  { assets, sourcemap }: MetroOptions,
): Plugin {
  checkWorklets(root);
  const configName = babelConfigNames.find((name) =>
    fs.existsSync(path.join(root, name)),
  );
  const babel: TransformOptions = {
    cwd: root,
    root,
    babelrc: false,
    configFile: false,
    sourceType: "unambiguous",
    envName: "production",
    compact: false,
    retainLines: true,
    sourceMaps: sourcemap ? "inline" : false,
    ...(configName
      ? { extends: path.join(root, configName) }
      : { presets: [defaultPreset(root)] }),
    plugins: [requireContext],
  };
  const caller: TransformCaller = {
    name: "metro",
    bundler: "metro",
    platform: "ios",
    engine: "hermes",
    isDev: false,
    isServer: false,
    isReactServer: false,
    baseUrl: "",
    routerRoot: "app",
    projectRoot: root,
    isHMREnabled: true,
    metroSourceType: "module",
    supportsStaticESM: true,
  };
  const internal = Symbol("internal");
  return {
    name: "swiplet-metro",
    setup(build) {
      build.onResolve({ filter: /^@babel\/runtime\// }, async (args) => {
        if (args.pluginData === internal) {
          return undefined;
        }
        const options = {
          kind: args.kind,
          importer: args.importer,
          pluginData: internal,
        };
        const found = await build.resolve(args.path, {
          ...options,
          resolveDir: args.resolveDir,
        });
        return found.errors.length === 0
          ? found
          : build.resolve(args.path, { ...options, resolveDir: cliDirectory });
      });
      if (assets) {
        build.onResolve(
          { filter: new RegExp(`\\.(${assetExtensions.join("|")})$`) },
          async (args) => {
            if (args.pluginData === internal) {
              return undefined;
            }
            const extension = path.extname(args.path);
            const stem = args.path.slice(0, -extension.length);
            let first: ResolveResult | undefined;
            for (const scale of scaleSuffixes) {
              for (const platform of platformSuffixes) {
                const found = await build.resolve(
                  `${stem}${scale}${platform}${extension}`,
                  {
                    kind: args.kind,
                    importer: args.importer,
                    resolveDir: args.resolveDir,
                    pluginData: internal,
                  },
                );
                if (found.errors.length === 0) {
                  return {
                    path: found.path,
                    namespace: "swiplet-asset",
                    pluginData: assetVariants(found.path),
                  };
                }
                first ??= found;
              }
            }
            return { errors: first?.errors };
          },
        );
        build.onLoad({ filter: /.*/, namespace: "swiplet-asset" }, (args) => ({
          contents: assetModule(root, args.pluginData, assets),
          loader: "js",
          resolveDir: root,
        }));
      }
      build.onLoad(
        { filter: /\.[cm]?[jt]sx?$/, namespace: "file" },
        async (args) => {
          const result = await transformAsync(
            await fs.promises.readFile(args.path, "utf8"),
            {
              ...babel,
              filename: args.path,
              caller: {
                ...caller,
                isNodeModule: args.path.includes("node_modules"),
              },
            },
          );
          const code = result?.code;
          return typeof code === "string"
            ? {
                contents: code,
                loader: loaderOf(args.path),
                resolveDir: path.dirname(args.path),
              }
            : undefined;
        },
      );
    },
  };
}
