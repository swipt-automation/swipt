import { spawn } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import readline from "node:readline";

import { label, sameName } from "./connection";
import { tr } from "./i18n";
import { type PromptContext, choose, interactive, step } from "./output";
import type { Passcode } from "./passcode";

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
    if ((error as NodeJS.ErrnoException).code !== "MODULE_NOT_FOUND") {
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

interface Failure {
  code?: string;
  message: string;
}

type Line = { result: unknown } | { error: Failure };

export class DriverError extends Error {
  declare code: string | undefined;

  constructor({ code, message }: Failure) {
    super(
      code === "usbmuxd_unavailable" && usbmuxdHint !== undefined
        ? `${usbmuxdHint} (${message})`
        : code === "driver_running"
          ? tr("The driver is already running on this computer; stop it first.")
          : message,
    );
    this.code = code;
  }
}

interface Invocation {
  input?: unknown;
  signal?: AbortSignal;
}

function start(args: string[], { input, signal }: Invocation = {}) {
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
  let last: Line | undefined;
  const { promise: first, resolve: firstLine } = Promise.withResolvers<
    Line | undefined
  >();
  readline.createInterface({ input: child.stdout }).on("line", (line) => {
    let parsed: Line;
    try {
      parsed = JSON.parse(line);
    } catch {
      parsed = { error: { message: line } };
    }
    last = parsed;
    firstLine(parsed);
  });
  const exited = new Promise<unknown>((resolve, reject) => {
    child.once("error", reject);
    child.once("close", (code, signalName) => {
      signal?.removeEventListener("abort", stop);
      child.stdin.end();
      firstLine(undefined);
      if (last !== undefined && "error" in last) {
        reject(new DriverError(last.error));
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

const driver = <Result = void>(args: string[], options?: Invocation) =>
  start(args, options).exited as Promise<Result>;

export interface Device {
  name: string;
  udid: string;
  paired: boolean;
  virtual: boolean;
}

export interface Application {
  bundleIdentifier: string;
  name: string;
  version: string;
  bundleVersion: string;
}

export interface Profile {
  name: string;
  uuid: string;
  teamIdentifier: string;
  creationDate: string;
  expirationDate: string;
}

interface Serving {
  device: string;
  udid: string;
  api: string;
  mcp: string;
  network?: string;
}

const onDevice = <Result = void>(
  device: Device,
  command: string,
  args: string[] = [],
  options?: Invocation,
) => driver<Result>(["device", command, "-d", device.udid, ...args], options);

export const randomToken = () => crypto.randomBytes(16).toString("hex");

export function serve(
  udid: string,
  {
    passcode,
    token = "",
    localNetwork = false,
    signal,
  }: {
    passcode?: Passcode;
    token?: string;
    localNetwork?: boolean;
    signal: AbortSignal;
  },
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
      if (line === undefined || !("result" in line)) {
        await running.exited;
        signal.throwIfAborted();
        throw new Error("swipt-driver stopped before serving");
      }
      const serving = line.result as Serving;
      const local = new URL(serving.api);
      local.hostname = "127.0.0.1";
      return { ...serving, local: { url: local.origin, token } };
    }),
    stderr: running.stderr,
    finished: () => running.exited,
    async stop() {
      running.stop();
      await running.exited.catch(() => {});
    },
  };
}

const deviceLine = (device: Device) => `  ${device.name}  ${device.udid}`;

interface DeviceQuery {
  required?: boolean;
  virtual?: boolean;
  filter?: (devices: Device[]) => Promise<Device[]> | Device[];
}

async function findDevice(
  context: PromptContext,
  { required = true, virtual = true, filter }: DeviceQuery,
) {
  const given = context.options.device;
  const listed = await step(
    tr("looking for devices"),
    () => driver<Device[]>(["device", "list"]),
    { transient: true },
  );
  const devices = listed
    .filter((device) => virtual || !device.virtual)
    .map((device) => ({ ...device, name: device.name || device.udid }));
  const matches = given
    ? devices.filter(
        (device) =>
          sameName(device.udid, given) ||
          sameName(label(device.name), label(given)),
      )
    : devices;
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

export async function trustedDevice(
  context: PromptContext,
  query: DeviceQuery = {},
) {
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

export const apps = (device: Device) => onDevice<Application[]>(device, "apps");

export const appRunning = (device: Device, id: string) =>
  onDevice<boolean>(device, "running", [id]);

export const installApp = (device: Device, app: string, id: string) =>
  onDevice(device, "install", [app, id]);

export const uninstallApp = (device: Device, id: string) =>
  onDevice(device, "uninstall", [id]);

export const launchApp = (device: Device, id: string, args: string[] = []) =>
  onDevice(device, "launch", [id, "--", ...args]);

export const openSettings = (device: Device, url: string) =>
  onDevice(device, "launch", ["--payload-url", url, "com.apple.Preferences"]);

export const terminateApp = (device: Device, id: string) =>
  onDevice(device, "terminate", [id]);

export const configureVirtual = (device: Device, id: string, name: string) =>
  onDevice(device, "configure", [id, name]);

export const pairApp = (device: Device, id: string, file: string) =>
  onDevice(device, "pair", [id, file]);

export const containerHasFile = (device: Device, id: string, file: string) =>
  onDevice<boolean>(device, "has", [id, file]);

export const containerSQL = (
  device: Device,
  id: string,
  file: string,
  statements: string,
  { query = false } = {},
) =>
  onDevice<Record<string, unknown>[]>(
    device,
    "sqlite",
    [id, file, ...(query ? ["--query"] : [])],
    { input: statements },
  );

export const provisioningProfiles = (device: Device) =>
  onDevice<Profile[]>(device, "profiles");

export const mountDiskImage = (device: Device) =>
  onDevice(device, "mount", ["--disk-image", diskImage()]);
