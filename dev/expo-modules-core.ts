// The npm package ships prebuilt Debug and Release xcframeworks, which Expo's prebuild generates from
// its spm.config.json (expo 0bbc67b, tools/src/prebuilds/SPMGenerator.ts, tools/src/prebuilds/SPMBuild.ts).
// Expo's SwiftPM plugin prefers an expanded prebuilds/output/<flavor>/xcframeworks/ExpoModulesCore.xcframework
// over the tarball (expo 58.0.22, scripts/spm/flavored-frameworks.js:35-54).

import { spawnSync } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const work = path.join(root, "build/expo-modules-core");
const product = "ExpoModulesCore";

interface Target {
  name: string;
  path: string;
  type: "cpp" | "objc" | "swift";
  exclude?: string[];
  fileMapping?: { from: string; to: string }[];
  dependencies?: string[];
  linkedFrameworks?: string[];
  includeDirectories?: string[];
  compilerFlags?: { common?: string[] };
}

interface Config {
  products: {
    name: string;
    platforms: string[];
    targets: Target[];
    excludeFromUmbrella?: string[];
  }[];
}

const flavors = { debug: "Debug", release: "Release" };
const platforms = [
  { sdk: "iphoneos", destination: "generic/platform=iOS", slice: "ios-arm64" },
  {
    sdk: "iphonesimulator",
    destination: "generic/platform=iOS Simulator",
    slice: "ios-arm64_x86_64-simulator",
  },
];

const environment = () => ({
  PATH: process.env.PATH,
  HOME: process.env.HOME,
  ...(process.env.DEVELOPER_DIR
    ? { DEVELOPER_DIR: process.env.DEVELOPER_DIR }
    : {}),
});

function run(
  command: string,
  args: string[],
  {
    cwd = root,
    env = process.env,
    log,
  }: { cwd?: string; env?: NodeJS.ProcessEnv; log?: string } = {},
) {
  const output = log === undefined ? "inherit" : fs.openSync(log, "w");
  let result;
  try {
    result = spawnSync(command, args, {
      cwd,
      env,
      stdio: ["ignore", output, output],
    });
  } finally {
    if (log !== undefined) {
      fs.closeSync(output as number);
    }
  }
  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    const where =
      log === undefined ? "" : `; the full log is ${path.relative(root, log)}`;
    throw new Error(
      `${command} ${args[0]} exited with ${result.status ?? `signal ${result.signal}`}${where}`,
    );
  }
}

function* walk(directory: string, base = directory): Generator<string> {
  for (const entry of fs
    .readdirSync(directory, { withFileTypes: true })
    .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0))) {
    const file = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      yield* walk(file, base);
    } else if (entry.isFile()) {
      yield path.relative(base, file);
    }
  }
}

function excluded(file: string, patterns: string[]) {
  return patterns.some((pattern) => {
    if (pattern.startsWith("**/") && pattern.endsWith("/**")) {
      return `/${file}`.includes(`/${pattern.slice(3, -3)}/`);
    }
    if (pattern.endsWith("/**")) {
      return file.startsWith(`${pattern.slice(0, -3)}/`);
    }
    return file === pattern;
  });
}

function sources(directory: string, extensions: string[], exclude: string[]) {
  return [...walk(directory)].filter(
    (file) =>
      extensions.includes(path.extname(file)) &&
      !excluded(file, [...exclude, "**/Tests/**"]),
  );
}

function link(source: string, destination: string) {
  fs.mkdirSync(path.dirname(destination), { recursive: true });
  fs.symlinkSync(source, destination);
}

function hardLink(source: string, destination: string) {
  fs.mkdirSync(path.dirname(destination), { recursive: true });
  try {
    fs.linkSync(source, destination);
  } catch {
    fs.copyFileSync(source, destination);
  }
}

const quote = (value: string | string[]) => JSON.stringify(value);

const resolvePackage = (name: string, from: string) =>
  path.dirname(
    createRequire(path.join(from, "package.json")).resolve(
      `${name}/package.json`,
    ),
  );

function stage(core: string, config: Config) {
  // SwiftPM names the package after its directory, and the interfaces record it as -package-name.
  const packageDir = path.join(work, product);
  fs.rmSync(packageDir, { recursive: true, force: true });
  const coreProduct = config.products.find(
    (candidate) => candidate.name === product,
  );
  if (coreProduct === undefined) {
    throw new Error(`spm.config.json has no product ${product}`);
  }
  const version = JSON.parse(
    fs.readFileSync(path.join(core, "package.json"), "utf8"),
  ).version;
  const macros = path.join(
    resolvePackage("expo-modules-macros", core),
    "apple/ExpoModulesMacros",
  );
  const internal = new Set(coreProduct.targets.map((target) => target.name));
  const headers: Record<string, string[]> = {};
  const targets: string[] = [];
  for (const target of coreProduct.targets) {
    const targetPath = path.join(core, target.path);
    const destination = path.join(packageDir, target.name);
    const exclude = target.exclude ?? [];
    const mapped = new Set<string>();
    for (const mapping of target.fileMapping ?? []) {
      mapped.add(mapping.from);
      link(
        path.join(targetPath, mapping.from),
        path.join(
          destination,
          mapping.to.replace("{filename}", path.basename(mapping.from)),
        ),
      );
    }
    const extensions = {
      cpp: [".cpp", ".c", ".cc", ".cxx"],
      objc: [".m", ".mm", ".c", ".cpp"],
      swift: [".swift"],
    }[target.type];
    for (const file of sources(targetPath, extensions, exclude)) {
      link(path.join(targetPath, file), path.join(destination, file));
    }
    const dependencies = (target.dependencies ?? [])
      .filter((dependency) => internal.has(dependency))
      .map(quote);
    const react = [
      '.product(name: "ReactHeaders", package: "ReactNative")',
      '.product(name: "ReactNativeHeaders", package: "ReactNative")',
      '.product(name: "ReactNativeDependenciesHeaders", package: "ReactNative")',
    ];
    if (target.type === "swift") {
      const frameworks = (target.linkedFrameworks ?? []).map(
        (framework) => `@_exported import ${framework}`,
      );
      const imports = (target.dependencies ?? [])
        .filter((dependency) => internal.has(dependency))
        .map((dependency) => `@_exported import ${dependency}`);
      fs.writeFileSync(
        path.join(destination, `${product}+Exports.swift`),
        `${[...frameworks, ...imports].join("\n")}\n`,
      );
      targets.push(`    .target(
      name: ${quote(target.name)},
      dependencies: [${[...dependencies, ...react].join(", ")}],
      path: ${quote(target.name)},
      swiftSettings: [
        .define("RCT_NEW_ARCH_ENABLED"),
        .define("RCT_REMOVE_LEGACY_ARCH"),
        .unsafeFlags(${quote(["-Xcc", "-fmodules", "-Xfrontend", "-load-plugin-executable", "-Xfrontend", `${macros}#ExpoModulesMacros`])}),
      ],
      linkerSettings: [${(target.linkedFrameworks ?? []).map((framework) => `.linkedFramework(${quote(framework)})`).join(", ")}]
    )`);
      continue;
    }
    headers[target.name] = [];
    for (const file of sources(targetPath, [".h"], exclude)) {
      if (mapped.has(file)) {
        continue;
      }
      hardLink(
        path.join(targetPath, file),
        path.join(destination, "include", product, path.basename(file)),
      );
      headers[target.name].push(path.basename(file));
    }
    const settings = [
      '.headerSearchPath("include")',
      `.headerSearchPath(${quote(`include/${product}`)})`,
      '.define("RCT_NEW_ARCH_ENABLED", to: "1")',
      '.define("RCT_REMOVE_LEGACY_ARCH", to: "1")',
      '.unsafeFlags(["-fmodules"])',
      `.unsafeFlags(${quote((target.includeDirectories ?? []).flatMap((directory) => ["-I", path.resolve(targetPath, directory)]))})`,
    ];
    for (const flag of target.compilerFlags?.common ?? []) {
      const match = flag
        .replace("${PACKAGE_VERSION}", version)
        .match(/^-D(\w+)(?:=(.+))?$/);
      if (match === null) {
        throw new Error(
          `${target.name} has a flag that defines nothing: ${flag}`,
        );
      }
      const [, name, value] = match;
      settings.push(
        value === undefined
          ? `.define(${quote(name)})`
          : `.define(${quote(name)}, to: ${quote(value)})`,
      );
    }
    targets.push(`    .target(
      name: ${quote(target.name)},
      dependencies: [${[...dependencies, ...react].join(", ")}],
      path: ${quote(target.name)},
      publicHeadersPath: "include",
      cSettings: [${settings.join(", ")}],
      cxxSettings: [${settings.join(", ")}],
      linkerSettings: [${(target.linkedFrameworks ?? []).map((framework) => `.linkedFramework(${quote(framework)})`).join(", ")}]
    )`);
  }
  const platform = coreProduct.platforms.map((entry) => `.${entry}`).join(", ");
  fs.writeFileSync(
    path.join(packageDir, "Package.swift"),
    `// swift-tools-version: 6.0
// Generated by dev/expo-modules-core.ts from ${path.relative(root, path.join(core, "spm.config.json"))}.
import PackageDescription

let package = Package(
  name: ${quote(product)},
  platforms: [${platform}],
  products: [
    .library(name: ${quote(product)}, type: .dynamic, targets: [${coreProduct.targets.map((target) => quote(target.name)).join(", ")}])
  ],
  dependencies: [
    .package(name: "ReactNative", path: ${quote(path.join(root, "build/xcframeworks"))})
  ],
  targets: [
${targets.join(",\n")}
  ],
  swiftLanguageModes: [.v6],
  cxxLanguageStandard: .cxx20
)
`,
  );
  return { packageDir, coreProduct, headers };
}

// ExpoModulesCore links the real ExpoModulesJSI; npm ships only its headers, and the app builds it
// in its "Build ExpoModulesJSI" phase (expo-modules-jsi, apple/scripts/build-swiftpm-app-phase.sh).
function buildJSI(core: string) {
  const jsi = resolvePackage("expo-modules-jsi", core);
  const reactNative = resolvePackage("react-native", root);
  const xcframeworks = path.join(root, "build/xcframeworks");
  const jsiRoot = path.join(work, "jsi");
  for (const { sdk, slice } of platforms) {
    const stageDir = path.join(jsiRoot, `pods-stage-${slice}`);
    const publicHeaders = path.join(stageDir, "Headers/Public");
    const reactHeaders = path.join(
      xcframeworks,
      "ReactNativeHeaders.xcframework",
      slice,
      "Headers",
    );
    const dependencyHeaders = path.join(
      xcframeworks,
      "ReactNativeDependenciesHeaders.xcframework",
      slice,
      "Headers",
    );
    fs.rmSync(publicHeaders, { recursive: true, force: true });
    for (const [source, name] of [
      [path.join(reactNative, "ReactCommon/jsi/jsi"), "React-jsi/jsi"],
      [
        path.join(reactNative, "ReactCommon/react/renderer/runtimescheduler"),
        "React-runtimescheduler/react/renderer/runtimescheduler",
      ],
      [
        path.join(reactNative, "ReactCommon/react/renderer/consistency"),
        "React-rendererconsistency/react/renderer/consistency",
      ],
      [
        path.join(reactNative, "ReactCommon/react/performance/timeline"),
        "React-performancetimeline/react/performance/timeline",
      ],
      [
        path.join(reactNative, "ReactCommon/react/timing"),
        "React-timing/react/timing",
      ],
      [
        path.join(reactNative, "ReactCommon/react/debug"),
        "React-debug/react/debug",
      ],
      [
        path.join(reactNative, "ReactCommon/callinvoker/ReactCommon"),
        "React-callinvoker/ReactCommon",
      ],
      [
        path.join(reactNative, "ReactCommon/runtimeexecutor/ReactCommon"),
        "React-runtimeexecutor/ReactCommon",
      ],
      [path.join(reactHeaders, "hermes"), "hermes-engine/hermes"],
      [dependencyHeaders, "ReactNativeDependencies"],
    ]) {
      link(source, path.join(publicHeaders, name));
    }
    const podspec = path.join(
      stageDir,
      "Local Podspecs/React-Core.podspec.json",
    );
    fs.mkdirSync(path.dirname(podspec), { recursive: true });
    const podspecJSON = JSON.stringify({
      name: "React-Core",
      version: JSON.parse(
        fs.readFileSync(path.join(reactNative, "package.json"), "utf8"),
      ).version,
    });
    if (
      !fs.existsSync(podspec) ||
      fs.readFileSync(podspec, "utf8") !== podspecJSON
    ) {
      fs.writeFileSync(podspec, podspecJSON);
    }
    run("bash", [path.join(jsi, "apple/scripts/build-xcframework.sh")], {
      env: {
        ...environment(),
        EXPO_CUSTOM_BUILD_ROOT: jsiRoot,
        PODS_ROOT: stageDir,
        REACT_NATIVE_PATH: reactNative,
        PLATFORM_NAME: sdk,
      },
      log: path.join(work, `jsi-${sdk}.log`),
    });
  }
  return path.join(jsiRoot, "Products/ExpoModulesJSI.xcframework");
}

function inputsDigest(core: string, jsi: string) {
  const digest = crypto.createHash("sha256");
  const add = (label: string, content: string | Buffer) =>
    digest.update(label).update("\0").update(content).update("\0");
  for (const directory of ["ios", "common"]) {
    for (const file of walk(path.join(core, directory))) {
      add(
        `${directory}/${file}`,
        fs.readFileSync(path.join(core, directory, file)),
      );
    }
  }
  for (const file of ["package.json", "spm.config.json"]) {
    add(file, fs.readFileSync(path.join(core, file)));
  }
  for (const { slice } of platforms) {
    add(`jsi/${slice}`, fs.readFileSync(path.join(jsi, slice, ".build-hash")));
  }
  add("script", fs.readFileSync(fileURLToPath(import.meta.url)));
  add(
    "react-native",
    fs.readFileSync(path.join(root, "node_modules/react-native/package.json")),
  );
  add(
    "xcode",
    spawnSync("xcodebuild", ["-version"], { encoding: "utf8" }).stdout ?? "",
  );
  return digest.digest("hex");
}

function compose(
  { packageDir, coreProduct, headers }: ReturnType<typeof stage>,
  jsi: string,
  flavor: string,
  configuration: string,
) {
  const derivedData = path.join(work, "DerivedData");
  const xcframeworks = path.join(root, "build/xcframeworks", flavor);
  const staged = [];
  for (const { sdk, destination, slice } of platforms) {
    const linked = [
      [path.join(xcframeworks, "React.xcframework", slice), "React"],
      [
        path.join(xcframeworks, "ReactNativeDependencies.xcframework", slice),
        "ReactNativeDependencies",
      ],
      [path.join(xcframeworks, "hermes-engine.xcframework", slice), "hermesvm"],
      [path.join(jsi, slice), "ExpoModulesJSI"],
    ];
    for (const [directory, name] of linked) {
      if (!fs.existsSync(path.join(directory, `${name}.framework`))) {
        throw new Error(
          `${path.join(directory, `${name}.framework`)} is missing; setup-apple-spm.js update installs React Native's artifacts`,
        );
      }
    }
    // Settings from Expo's prebuild (tools/src/prebuilds/SPMBuild.ts) and the ExpoModulesJSI build.
    run(
      "xcodebuild",
      [
        "build",
        "-scheme",
        product,
        "-destination",
        destination,
        "-configuration",
        configuration,
        "-derivedDataPath",
        derivedData,
        "-skipMacroValidation",
        "-skipPackagePluginValidation",
        `SYMROOT=${path.join(derivedData, "Build/Products")}`,
        `OBJROOT=${path.join(derivedData, "Build/Intermediates.noindex")}`,
        "BUILD_LIBRARY_FOR_DISTRIBUTION=YES",
        "SKIP_INSTALL=NO",
        "ONLY_ACTIVE_ARCH=NO",
        "DEBUG_INFORMATION_FORMAT=dwarf-with-dsym",
        "STRIP_SWIFT_SYMBOLS=NO",
        "COMPILER_INDEX_STORE_ENABLE=NO",
        "SWIFT_COMPILATION_MODE=wholemodule",
        "CLANG_ENABLE_CODE_COVERAGE=NO",
        "CLANG_COVERAGE_MAPPING=NO",
        "SWIFT_SERIALIZE_DEBUGGING_OPTIONS=NO",
        "SWIFT_ENABLE_EXPLICIT_MODULES=NO",
        // SwiftPM targets do not get Xcode's default project definitions; React Native's headers
        // differ without NDEBUG, so Release would reference symbols its React.framework lacks.
        `GCC_PREPROCESSOR_DEFINITIONS=$(inherited) ${configuration === "Release" ? "NDEBUG=1 NS_BLOCK_ASSERTIONS=1" : "DEBUG=1"}`,
        "OTHER_SWIFT_FLAGS=$(inherited) -no-verify-emitted-module-interface",
        `FRAMEWORK_SEARCH_PATHS=$(inherited) ${quote(path.join(jsi, slice))}`,
        `OTHER_LDFLAGS=$(inherited) ${linked.map(([directory, name]) => `-F${quote(directory)} -framework ${name}`).join(" ")}`,
      ],
      {
        cwd: packageDir,
        env: environment(),
        log: path.join(work, `${flavor}-${sdk}.log`),
      },
    );

    const products = path.join(
      derivedData,
      "Build/Products",
      `${configuration}-${sdk}`,
    );
    const framework = path.join(
      work,
      "stage",
      flavor,
      slice,
      `${product}.framework`,
    );
    fs.rmSync(path.dirname(framework), { recursive: true, force: true });
    fs.mkdirSync(path.dirname(framework), { recursive: true });
    run("cp", [
      "-a",
      path.join(products, "PackageFrameworks", `${product}.framework`),
      framework,
    ]);

    const headersDir = path.join(framework, "Headers");
    fs.mkdirSync(headersDir, { recursive: true });
    const umbrella = [];
    const excludeFromUmbrella = new Set(coreProduct.excludeFromUmbrella ?? []);
    for (const target of coreProduct.targets) {
      for (const header of [...(headers[target.name] ?? [])].sort((a, b) =>
        a < b ? -1 : a > b ? 1 : 0,
      )) {
        fs.copyFileSync(
          path.join(packageDir, target.name, "include", product, header),
          path.join(headersDir, header),
        );
        if (!excludeFromUmbrella.has(header)) {
          umbrella.push(`#import "${header}"`);
        }
      }
    }
    fs.writeFileSync(
      path.join(headersDir, `${product}_umbrella.h`),
      umbrella.join("\n"),
    );

    const internal = coreProduct.targets
      .map((target) => target.name)
      .filter((name) => name !== product);
    let swiftHeader = fs.readFileSync(
      path.join(
        derivedData,
        "Build/Intermediates.noindex",
        `GeneratedModuleMaps-${sdk}`,
        `${product}-Swift.h`,
      ),
      "utf8",
    );
    for (const name of internal) {
      swiftHeader = swiftHeader.replace(
        new RegExp(`^@import ${name};\\s*$`, "gm"),
        `// Removed internal target import: ${name}`,
      );
    }
    swiftHeader = swiftHeader.replace(
      /^@import React;\s*$/gm,
      "// Removed: @import React;",
    );
    fs.writeFileSync(path.join(headersDir, `${product}-Swift.h`), swiftHeader);

    const modules = path.join(framework, "Modules");
    const swiftModule = path.join(modules, `${product}.swiftmodule`);
    fs.mkdirSync(swiftModule, { recursive: true });
    const builtModule = path.join(products, `${product}.swiftmodule`);
    for (const entry of fs.readdirSync(builtModule, { withFileTypes: true })) {
      if (!entry.isFile() || entry.name.endsWith(".swiftmodule")) {
        continue;
      }
      let content = fs.readFileSync(path.join(builtModule, entry.name), "utf8");
      if (entry.name.endsWith(".swiftinterface")) {
        content = content.replace(
          /^(\/\/ swift-module-flags-ignorable:.*) -no-verify-emitted-module-interface/m,
          "$1",
        );
        for (const name of internal) {
          content = content
            .replace(
              new RegExp(`^@_exported import ${name}\\s*$`, "gm"),
              `@_exported import ${product}`,
            )
            .replace(
              new RegExp(`^import ${name}\\s*$`, "gm"),
              `import ${product}`,
            )
            .replace(new RegExp(`\\b${name}(::|\\.)`, "g"), `${product}$1`);
        }
      }
      fs.writeFileSync(path.join(swiftModule, entry.name), content);
    }
    fs.writeFileSync(
      path.join(modules, "module.modulemap"),
      `framework module ${product} {
    use React
    umbrella header "${product}_umbrella.h"
    header "${product}-Swift.h"

    export *
    module * { export * }
}
`,
    );
    const symbols = [
      path.join(products, "PackageFrameworks", `${product}.framework.dSYM`),
      path.join(products, `${product}.framework.dSYM`),
    ].find((candidate) => fs.existsSync(candidate));
    staged.push({ framework, symbols });
  }

  const target = path.join(
    fs.realpathSync(path.join(root, "node_modules/expo-modules-core")),
    "prebuilds/output",
    flavor,
    "xcframeworks",
    `${product}.xcframework`,
  );
  const temporary = path.join(work, "stage", flavor, `${product}.xcframework`);
  fs.rmSync(temporary, { recursive: true, force: true });
  run(
    "xcodebuild",
    [
      "-create-xcframework",
      ...staged.flatMap(({ framework, symbols }) => [
        "-framework",
        framework,
        ...(symbols ? ["-debug-symbols", symbols] : []),
      ]),
      "-output",
      temporary,
    ],
    {
      log: path.join(work, `${flavor}-xcframework.log`),
    },
  );
  fs.rmSync(target, { recursive: true, force: true });
  fs.mkdirSync(path.dirname(target), { recursive: true });
  fs.renameSync(temporary, target);
  return target;
}

export function buildExpoModulesCore() {
  const core = fs.realpathSync(
    path.join(root, "node_modules/expo-modules-core"),
  );
  const config: Config = JSON.parse(
    fs.readFileSync(path.join(core, "spm.config.json"), "utf8"),
  );
  fs.mkdirSync(work, { recursive: true });
  const jsi = buildJSI(core);
  const stamp = inputsDigest(core, jsi);
  const stampFile = path.join(work, "inputs");
  const targets = Object.keys(flavors).map((flavor) =>
    path.join(
      core,
      "prebuilds/output",
      flavor,
      "xcframeworks",
      `${product}.xcframework`,
    ),
  );
  if (
    targets.every((target) => fs.existsSync(path.join(target, "Info.plist"))) &&
    fs.existsSync(stampFile) &&
    fs.readFileSync(stampFile, "utf8") === stamp
  ) {
    return false;
  }
  fs.rmSync(stampFile, { force: true });
  const staged = stage(core, config);
  for (const [flavor, configuration] of Object.entries(flavors)) {
    console.log(`Building ${product} ${configuration}`);
    compose(staged, jsi, flavor, configuration);
  }
  fs.writeFileSync(stampFile, stamp);
  return true;
}
