import { execFileSync, spawnSync } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";

import { androidSwiptApp } from "@swipt/cli/adb";
import { FalseResult, errorMessage } from "@swipt/cli/output";
import { validatePasscodeType } from "@swipt/cli/passcode";
import {
  installAndOpen,
  installPasscode,
  targetDevice,
} from "@swipt/cli/sideload";
import { bundleId, swiptApps } from "@swipt/cli/swipt-app";

import { buildExpoModulesCore } from "./expo-modules-core";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const iosApp = path.join(
  root,
  "build/DerivedData/Build/Products/Debug-iphoneos/Swipt.app",
);
const androidAar = "android/app/libs/libswipt.aar";
const androidApk = path.join(
  root,
  "android/app/build/outputs/apk/debug/app-debug.apk",
);
const androidMetadata = path.join(
  root,
  "android/app/build/outputs/apk/debug/output-metadata.json",
);
const builds = path.join(root, "build/dev-install.json");
const goSources = [
  "Makefile",
  "go.mod",
  "go.sum",
  "api/*.go",
  "api/package.json",
  "internal",
  ":!internal/automation/runtime",
  ":!*_test.go",
];
const runtimeSources = [
  "Makefile",
  "package.json",
  "bun.lock",
  "bunfig.toml",
  "api",
  "internal/automation/runtime",
  "internal/automation/bundles",
  "internal/automation/rewrite.js",
  "internal/automation/declarations",
];

const driver = path.join(
  root,
  "build/driver",
  `${process.platform}-${process.arch}`,
  process.platform === "win32" ? "swipt-driver.exe" : "swipt-driver",
);

const interrupt = new AbortController();
process.on("SIGINT", () => interrupt.abort());

function run(command: string, args: string[], env?: NodeJS.ProcessEnv) {
  const result = spawnSync(command, args, {
    cwd: root,
    env: { ...process.env, ...env },
    stdio: "inherit",
  });
  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    throw new Error(
      `${[command, ...args].join(" ")} exited with ${result.status ?? `signal ${result.signal}`}`,
    );
  }
}

const capture = (command: string, ...args: string[]) =>
  execFileSync(command, args, { cwd: root, encoding: "utf8" });

function committed(paths: string[]) {
  const status = capture(
    "git",
    "status",
    "--porcelain",
    "--untracked-files=all",
    "--ignore-submodules=none",
    "--",
    ...paths,
  );
  return status === ""
    ? capture("git", "ls-files", "--stage", "--", ...paths)
    : undefined;
}

const modified = (file: string | undefined) =>
  file === undefined
    ? undefined
    : fs.statSync(path.resolve(root, file), { throwIfNoEntry: false })?.mtimeMs;

function build(
  name: string,
  {
    paths,
    inputs = [],
    product,
  }: { paths: string[]; inputs?: string[]; product?: string },
  command: () => void,
) {
  const records = fs.existsSync(builds)
    ? JSON.parse(fs.readFileSync(builds, "utf8"))
    : {};
  const save = () => {
    fs.mkdirSync(path.dirname(builds), { recursive: true });
    fs.writeFileSync(builds, `${JSON.stringify(records, null, 2)}\n`);
  };
  const files = committed(paths);
  const key =
    files === undefined
      ? undefined
      : crypto
          .createHash("sha256")
          .update([files, ...inputs].join("\0"))
          .digest("hex");
  if (
    key !== undefined &&
    records[name]?.key === key &&
    records[name].product === modified(product)
  ) {
    console.log(`${name} is up to date`);
    return;
  }
  delete records[name];
  save();
  command();
  if (key !== undefined && committed(paths) === files) {
    records[name] = { key, product: modified(product) };
    save();
  }
}

const frameworks = () => {
  run("bun", ["install", "--frozen-lockfile"]);
  const update = () =>
    run("node", [
      "node_modules/react-native/scripts/setup-apple-spm.js",
      "update",
      "--product-name",
      "Swipt",
      "--yes",
    ]);
  update();
  if (buildExpoModulesCore()) {
    update();
  }
};

async function installApp(context: Parameters<typeof installPasscode>[0]) {
  validatePasscodeType(context.options);
  const goVersion = capture("go", "version");
  build(
    "driver",
    {
      paths: [...goSources, "cmd/swipt-driver", "cli/package.json"],
      inputs: [goVersion],
      product: driver,
    },
    () => run("make", ["driver"]),
  );
  const device = await targetDevice(context, { required: false });
  if (device.platform === "android") {
    const current = await androidSwiptApp(device, context.signal);
    const passcode = await installPasscode(context, device, current);
    run("bun", ["install", "--frozen-lockfile"]);
    build("runtime", { paths: runtimeSources }, () => run("make", ["runtime"]));
    build(
      "lib_android",
      {
        paths: [...goSources, "libswipt"],
        inputs: [goVersion],
        product: androidAar,
      },
      () => run("make", ["-o", "runtime", "lib_android"]),
    );
    build(
      "app_android",
      {
        paths: [
          "Makefile",
          "package.json",
          "bun.lock",
          "android",
          "internal/android",
          "internal/automation/runtime",
        ],
        inputs: [goVersion, String(modified(androidAar) ?? "")],
        product: androidApk,
      },
      () => run("make", ["-o", "lib_android", "build_android"]),
    );
    const metadata = JSON.parse(fs.readFileSync(androidMetadata, "utf8")) as {
      elements: { versionCode: number }[];
    };
    await installAndOpen(
      context,
      { device, id: bundleId, current },
      androidApk,
      metadata.elements[0].versionCode,
      { passcode },
    );
    return;
  }

  const toolchain = [goVersion, capture("xcodebuild", "-version")];
  const current = (await swiptApps(device)).find(
    (it) => it.bundleIdentifier === bundleId,
  );
  const passcode = await installPasscode(context, device, current);
  const flags = [
    process.env.XCODEBUILD_FLAGS,
    ...(device.virtual ? ["CODE_SIGNING_ALLOWED=NO"] : []),
  ].filter((flag) => flag !== undefined);
  const info = path.join(iosApp, "Info.plist");
  run("bun", ["install", "--frozen-lockfile"]);
  build("runtime", { paths: runtimeSources }, () => run("make", ["runtime"]));
  build(
    "lib",
    {
      paths: [...goSources, "libswipt"],
      inputs: toolchain,
      product: "Frameworks/LibSwipt.xcframework/Info.plist",
    },
    () => run("make", ["-o", "runtime", "lib"]),
  );
  build(
    "app",
    {
      paths: [
        ".",
        ":!.github",
        ":!cli",
        ":!cmd",
        ":!android",
        ":!internal/android",
        ":!*.md",
        ":!LICENSE",
        ":!.gitignore",
        ":!.golangci.yml",
        ":!.prettierignore",
        ":!*_test.go",
      ],
      inputs: [...toolchain, ...flags],
      product: info,
    },
    () => run("make", ["build_ios"], { XCODEBUILD_FLAGS: flags.join(" ") }),
  );
  const version = Number(
    capture("plutil", "-extract", "CFBundleVersion", "raw", "-o", "-", info),
  );
  await installAndOpen(
    context,
    { device, id: bundleId, current },
    iosApp,
    version,
    { passcode },
  );
}

const commands = [
  ["frameworks", "Install the React Native frameworks"],
  ["install [-d NAME|ID]", "Build Swipt and install it on the device"],
];

const usage = `usage: bun dev COMMAND

commands:
${commands.map(([use, help]) => `  ${use.padEnd(26)}${help}`).join("\n")}`;

async function main() {
  const { values: options, positionals } = parseArgs({
    allowPositionals: true,
    options: {
      device: { type: "string", short: "d" },
      passcode: { type: "string" },
      "passcode-type": { type: "string" },
      "no-setup": { type: "boolean" },
      "no-setup-pair": { type: "boolean" },
      "no-setup-api": { type: "boolean" },
    },
  });
  const [command] = positionals;
  switch (command) {
    case "frameworks":
      frameworks();
      break;
    case "install":
      await installApp({ options, signal: interrupt.signal });
      break;
    default:
      console.log(usage);
      process.exitCode = command === undefined ? 0 : 2;
  }
}

try {
  await main();
} catch (error) {
  if (error instanceof FalseResult) {
    process.exitCode = 1;
  } else if (
    interrupt.signal.aborted ||
    (error instanceof Error && error.name === "ExitPromptError")
  ) {
    process.exitCode = 130;
  } else {
    console.error("error:", errorMessage(error));
    process.exitCode = 1;
  }
}
