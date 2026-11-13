import { spawn } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import readline from "node:readline";

import { label, sameName } from "./connection";
import { tr } from "./i18n";
import { choose, interactive, step } from "./output";

const require = createRequire(import.meta.url);
const target = `${process.platform}-${process.arch}`;
const executable =
  process.platform === "win32" ? "swipt-driver.exe" : "swipt-driver";
const checkout = path.resolve(import.meta.dirname, "../..");
const fromSource = fs.existsSync(path.join(checkout, "go.mod"));
const crashLogLimit = 64 * 1024;

export const failures = {
  deviceNotListed: "device_not_listed",
  locked: "locked",
  untrusted: "untrusted",
};

function driverPath() {
  if (fromSource) {
    return path.join(checkout, "build/driver", target, executable);
  }
  try {
    return path.join(
      path.dirname(require.resolve(`@swipt/driver-${target}/package.json`)),
      executable,
    );
  } catch (error) {
    if (error.code !== "MODULE_NOT_FOUND") {
      throw error;
    }
    return undefined;
  }
}

const diskImage = () =>
  path.dirname(require.resolve("@swipt/developer-disk-image/package.json"));

const usbmuxdHint =
  process.platform === "linux"
    ? tr("usbmuxd isn't running; install usbmuxd and start it")
    : process.platform === "win32"
      ? tr(
          "Apple Mobile Device Service isn't running; install Apple Devices from the Microsoft Store",
        )
      : undefined;

function driverError({ code, message }) {
  let text = message;
  if (code === "usbmuxd_unavailable" && usbmuxdHint !== undefined) {
    text = `${usbmuxdHint} (${message})`;
  } else if (code === "driver_running") {
    text = tr("The driver is already running on this computer; stop it first.");
  }
  return Object.assign(new Error(text), { code });
}

function start(args, { input = undefined, signal = undefined } = {}) {
  const file = driverPath();
  if (file === undefined || !fs.existsSync(file)) {
    throw new Error(
      fromSource
        ? `${file} is missing; build it with "make driver".`
        : tr(
            'The driver is missing; install it with "npm install -g @swipt/cli --include=optional".',
          ),
    );
  }
  const child = spawn(file, args, {
    stdio: ["pipe", "pipe", "pipe"],
    windowsHide: true,
  });
  let stopped = false;
  const stop = () => {
    stopped = true;
    child.stdin.end();
  };
  child.stdin.on("error", () => {});
  if (input !== undefined) {
    child.stdin.write(`${JSON.stringify(input)}\n`);
  }
  signal?.addEventListener("abort", stop, { once: true });
  let logs = "";
  child.stderr.setEncoding("utf8").on("data", (chunk) => {
    logs = (logs + chunk).slice(-crashLogLimit);
  });
  const lines = [];
  let firstLine;
  const first = new Promise<any>((resolve) => {
    firstLine = resolve;
  });
  readline.createInterface({ input: child.stdout }).on("line", (line) => {
    let parsed;
    try {
      parsed = JSON.parse(line);
    } catch {
      parsed = { error: { message: line } };
    }
    lines.push(parsed);
    firstLine(parsed);
  });
  const exited = new Promise<any>((resolve, reject) => {
    child.once("error", reject);
    child.once("close", (code, signalName) => {
      signal?.removeEventListener("abort", stop);
      child.stdin.end();
      firstLine(undefined);
      const last = lines.at(-1);
      if (last?.error !== undefined) {
        reject(driverError(last.error));
      } else if (code === 0 || stopped) {
        resolve(last?.result);
      } else {
        reject(
          new Error(
            `swipt-driver exited with ${code ?? signalName}${logs.trim() === "" ? "" : `:\n${logs.trim()}`}`,
          ),
        );
      }
    });
  });
  exited.catch(() => {});
  return { first, exited, stop, stderr: child.stderr };
}

const driver = async (args, options = {}) => start(args, options).exited;

const onDevice = (device, command, args = [], options = {}) =>
  driver(["device", command, "-d", device.udid, ...args], options);

export const randomToken = () => crypto.randomBytes(16).toString("hex");

export function serve(
  udid,
  { passcode = undefined, token = "", localNetwork = false, signal },
) {
  const running = start(
    [
      "-d",
      udid,
      "--disk-image",
      diskImage(),
      "--secrets-stdin",
      ...(passcode ? ["--passcode-type", passcode.type] : []),
      ...(localNetwork ? ["--local-network"] : []),
    ],
    { input: { passcode: passcode?.passcode, token }, signal },
  );
  return {
    serving: running.first.then(async (line) => {
      if (line?.result === undefined) {
        await running.exited;
        signal.throwIfAborted();
        throw new Error("swipt-driver stopped before serving");
      }
      const local = new URL(line.result.api);
      local.hostname = "127.0.0.1";
      return { ...line.result, local: { url: local.origin, token } };
    }),
    stderr: running.stderr,
    finished: () => running.exited,
    async stop() {
      running.stop();
      await running.exited.catch(() => {});
    },
  };
}

const deviceLine = (device) => `  ${device.name}  ${device.udid}`;

interface DeviceQuery {
  required?: boolean;
  virtual?: boolean;
  filter?: (devices: any[]) => Promise<any[]> | any[];
}

async function findDevice(
  context,
  { required = true, virtual = true, filter }: DeviceQuery,
) {
  const given = context.options.device;
  const listed = await step(
    tr("looking for devices"),
    () => driver(["device", "list"]),
    { transient: true },
  );
  const devices = listed
    .filter((device) => virtual || !device.virtual)
    .map((device) => ({ ...device, name: device.name || device.udid }));
  const named = (device) =>
    sameName(device.udid, given) || sameName(label(device.name), label(given));
  const matches = given ? devices.filter(named) : devices;
  if ((given || !required) && matches.length === 1) {
    return matches[0];
  }
  if (devices.length === 0) {
    throw new Error(
      tr(
        "No iPhone or iPad is connected to this computer or found on the local network; connect one with a USB cable and unlock it.",
      ),
    );
  }
  const reason = !given
    ? tr("Choose a device with -d NAME|UDID")
    : matches.length === 0
      ? tr("No iPhone or iPad is named {name}", { name: given })
      : tr("More than one iPhone or iPad is named {name}; choose one by UDID", {
          name: given,
        });
  const failed = new Error(
    `${tr("{reason}; connected iPhone and iPad devices:", { reason })}\n${devices.map(deviceLine).join("\n")}`,
  );
  if (matches.length === 0) {
    throw failed;
  }
  const choices =
    filter !== undefined && interactive(context)
      ? await filter(matches)
      : matches;
  return choose(
    context,
    tr("Choose a device"),
    choices.map((device) => ({
      name: `${device.name}  ${device.udid}`,
      short: device.name,
      value: device,
    })),
    failed,
  );
}

export async function trustedDevice(context, query: DeviceQuery = {}) {
  const device = await findDevice(context, query);
  if (!device.paired) {
    await step(
      device.virtual
        ? tr("pairing with {device}", { device: device.name })
        : tr(
            "tap Trust on {device} and enter its passcode to trust this computer",
            { device: device.name },
          ),
      () => onDevice(device, "trust"),
      { transient: device.virtual },
    );
  }
  return device;
}

export const apps = (device) => onDevice(device, "apps");

export const appRunning = (device, id) => onDevice(device, "running", [id]);

export const installApp = (device, app, id) =>
  onDevice(device, "install", [app, id]);

export const uninstallApp = (device, id) => onDevice(device, "uninstall", [id]);

export const launchApp = (device, id, args = []) =>
  onDevice(device, "launch", [id, "--", ...args]);

export const openSettings = (device, url) =>
  onDevice(device, "launch", ["--payload-url", url, "com.apple.Preferences"]);

export const terminateApp = (device, id) => onDevice(device, "terminate", [id]);

export const configureVirtual = (device, id, name) =>
  onDevice(device, "configure", [id, name]);

export const pairApp = (device, id, file) =>
  onDevice(device, "pair", [id, file]);

export const containerHasFile = (device, id, file) =>
  onDevice(device, "has", [id, file]);

export const containerSQL = (
  device,
  id,
  file,
  statements,
  { query = false } = {},
) =>
  onDevice(device, "sqlite", [id, file, ...(query ? ["--query"] : [])], {
    input: statements,
  });

export const provisioningProfiles = (device) => onDevice(device, "profiles");

export const mountDiskImage = (device) =>
  onDevice(device, "mount", ["--disk-image", diskImage()]);
