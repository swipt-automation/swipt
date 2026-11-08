import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";

import { apps, coreDevice } from "@swipt/cli/coredevice";
import { FalseResult } from "@swipt/cli/output";
import {
  bundleId,
  installAndOpen,
  installPasscode,
} from "@swipt/cli/sideload";

import { buildExpoModulesCore } from "./expo-modules-core";
import { syncLocales } from "./strings";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const app = path.join(
  root,
  "build/DerivedData/Build/Products/Debug-iphoneos/Swipt.app",
);

const interrupt = new AbortController();
process.on("SIGINT", () => interrupt.abort());
process.env.PATH = `${path.join(root, "build")}${path.delimiter}${process.env.PATH}`;

function run(command, args, env?) {
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

const frameworks = () => {
  run("bun", ["install", "--frozen-lockfile"]);
  // ExpoModulesCore builds against the frameworks update links, and update links the ExpoModulesCore it finds.
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

async function installApp(context) {
  const device = await coreDevice(context, { required: false });
  const current = (await apps(device)).find(
    (it) => it.bundleIdentifier === bundleId,
  );
  const passcode = await installPasscode(context, device, current);
  const stamp = Math.floor(Date.now() / 1000);
  run("make", ["lib", "server", "build_ios"], {
    XCODEBUILD_FLAGS: [
      process.env.XCODEBUILD_FLAGS,
      `CURRENT_PROJECT_VERSION=${stamp}`,
      ...(device.virtual ? ["CODE_SIGNING_ALLOWED=NO"] : []),
    ]
      .filter(Boolean)
      .join(" "),
  });
  await installAndOpen(context, { device, id: bundleId, current }, app, stamp, {
    passcode,
  });
}

const commands = [
  ["frameworks", "Install the React Native frameworks"],
  ["install [-d NAME|UDID]", "Build Swipt and install it on the device"],
  ["strings", "Sync cli/locales with the strings of the CLI"],
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
    case "strings":
      syncLocales(path.join(root, "cli"));
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
  } else if (interrupt.signal.aborted || error.name === "ExitPromptError") {
    process.exitCode = 130;
  } else {
    console.error("error:", error.message);
    process.exitCode = 1;
  }
}
