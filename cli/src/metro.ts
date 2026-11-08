import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { transformAsync } from "@babel/core";
import { loadProjectEnv } from "@expo/env";
import { imageSize } from "image-size";

import { tr } from "./i18n";

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
const platformRank = { ios: 2, native: 1 };

const posix = (file) => file.split(path.sep).join("/");
const escape = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

export function loadEnvironment(root) {
  loadProjectEnv(root, { mode: "production", silent: true });
  process.env.BABEL_ENV ??= "production";
}

function defaultPreset(root) {
  try {
    return createRequire(path.join(root, "package.json")).resolve(
      "expo/internal/babel-preset",
    );
  } catch {
    return require.resolve("babel-preset-expo");
  }
}

function contextFiles(directory, recursive) {
  if (!fs.existsSync(directory)) {
    return [];
  }
  const extensions = new Set([...sourceExtensions, ...assetExtensions]);
  return fs
    .readdirSync(directory, { recursive })
    .map(posix)
    .filter(
      (file) =>
        extensions.has(path.extname(file).slice(1)) &&
        fs.statSync(path.join(directory, file)).isFile(),
    )
    .sort();
}

function requireContext({ types: t, template }) {
  const argument = (call, index, fallback, expected, check) => {
    if (call.node.arguments[index] === undefined) {
      if (fallback !== undefined) {
        return fallback;
      }
    } else {
      const { confident, value } = call.get(`arguments.${index}`).evaluate();
      if (confident && check(value)) {
        return value;
      }
    }
    throw call.buildCodeFrameError(
      tr("argument {index} of require.context must be {expected}", {
        index: index + 1,
        expected,
      }),
    );
  };
  const pattern = (call, index) => {
    const node = call.node.arguments[index];
    if (node === undefined) {
      return /^\.\/.*$/;
    }
    if (!t.isRegExpLiteral(node)) {
      throw call.buildCodeFrameError(
        tr(
          "argument {index} of require.context must be a regular expression literal",
          { index: index + 1 },
        ),
      );
    }
    return new RegExp(node.pattern, node.flags.replace(/[gy]/g, ""));
  };
  return {
    name: "swipt-require-context",
    visitor: {
      CallExpression(call, state) {
        const { callee } = call.node;
        if (
          !t.isMemberExpression(callee) ||
          !t.isIdentifier(callee.object, { name: "require" }) ||
          call.scope.hasBinding("require") ||
          !(callee.computed
            ? t.isStringLiteral(callee.property, { value: "context" })
            : t.isIdentifier(callee.property, { name: "context" }))
        ) {
          return;
        }
        const from = path.dirname(state.filename);
        const directory = path.resolve(
          from,
          argument(
            call,
            0,
            undefined,
            tr("a string"),
            (value) => typeof value === "string",
          ),
        );
        const recursive = argument(
          call,
          1,
          true,
          tr("a boolean"),
          (value) => typeof value === "boolean",
        );
        const filter = pattern(call, 2);
        const mode = argument(
          call,
          3,
          "sync",
          tr("one of {values}", { values: contextModes.join(", ") }),
          (value) => contextModes.includes(value),
        );
        const entries = contextFiles(directory, recursive)
          .map((file) => `./${file}`)
          .filter((key) => filter.test(key))
          .map((key) => {
            const target = posix(
              path.relative(from, path.join(directory, key)),
            );
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
}

const loaderOf = (file) => {
  const extension = path.extname(file);
  return extension === ".tsx"
    ? "tsx"
    : [".ts", ".mts", ".cts"].includes(extension)
      ? "ts"
      : "jsx";
};

function archivedAsset(root, file) {
  const relative = path.relative(root, file).split(path.sep);
  return relative[0] === "assets"
    ? { path: relative.slice(1).join("/"), bundled: false }
    : {
        path: relative.map((part) => (part === ".." ? "__" : part)).join("/"),
        bundled: true,
      };
}

function assetVariants(resolved) {
  const directory = path.dirname(resolved);
  const extension = path.extname(resolved);
  const stem = path
    .basename(resolved, extension)
    .replace(/(\.(ios|native))$/, "")
    .replace(/@[\d.]+x$/, "");
  const pattern = new RegExp(
    `^${escape(stem)}(?:@([\\d.]+)x)?(?:\\.(ios|native))?${escape(extension)}$`,
  );
  const scales = new Map();
  for (const name of fs.readdirSync(directory)) {
    const match = pattern.exec(name);
    if (match === null) {
      continue;
    }
    const scale = match[1] === undefined ? 1 : Number(match[1]);
    const rank = platformRank[match[2]] ?? 0;
    if (Number.isFinite(scale) && (scales.get(scale)?.rank ?? -1) < rank) {
      scales.set(scale, { rank, file: path.join(directory, name) });
    }
  }
  return [...scales]
    .sort(([a], [b]) => a - b)
    .map(([scale, { file }]) => ({ scale, file }));
}

function assetModule(root, variants, assets) {
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

function checkWorklets(root) {
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
        "the project has react-native-worklets {version}, but Swipt runs {expected}; install react-native-worklets@{expected} or remove it",
        { version, expected },
      ),
    );
  }
}

interface MetroOptions {
  assets?: Map<string, string>;
  sourcemap?: boolean;
}

export function metro(root, { assets, sourcemap }: MetroOptions) {
  checkWorklets(root);
  const configName = babelConfigNames.find((name) =>
    fs.existsSync(path.join(root, name)),
  );
  const babel = {
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
  const caller = {
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
            let first;
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
            return { errors: first.errors };
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
          return result === null
            ? undefined
            : {
                contents: result.code,
                loader: loaderOf(args.path),
                resolveDir: path.dirname(args.path),
              };
        },
      );
    },
  };
}
