import { execFile } from "node:child_process";
import fs from "node:fs";
import { createRequire } from "node:module";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { promisify } from "node:util";

import { label, sameName } from "./connection";
import { tr } from "./i18n";
import { choose, interactive, step } from "./output";

const exec = promisify(execFile);

export const driverHint = tr(
  'swipt-driver is missing; install it with "npm install -g @swipt/cli --include=optional".',
);

const driverName =
  process.platform === "win32" ? "swipt-driver.exe" : "swipt-driver";

export function driverPath() {
  const built = path.resolve(import.meta.dirname, "../../build", driverName);
  if (fs.existsSync(built)) {
    return built;
  }
  let packaged;
  try {
    packaged = path.join(
      path.dirname(
        createRequire(import.meta.url).resolve(
          `@swipt/driver-${process.platform}-${process.arch}/package.json`,
        ),
      ),
      driverName,
    );
  } catch (error) {
    if (error.code !== "MODULE_NOT_FOUND") {
      throw error;
    }
    return driverName;
  }
  return fs.existsSync(packaged) ? packaged : driverName;
}

export class MissingDriverError extends Error {
  constructor() {
    super(driverHint);
  }
}

const usbmuxdHint =
  process.platform === "linux"
    ? tr("usbmuxd isn't running; install usbmuxd and start it")
    : process.platform === "win32"
      ? tr(
          "Apple Mobile Device Service isn't running; install Apple Devices from the Microsoft Store",
        )
      : undefined;

export function driverError(stderr: string, fallback: string) {
  const message = /^Error: (.*)$/m.exec(stderr)?.[1] ?? fallback;
  return new Error(
    usbmuxdHint !== undefined && /^usbmuxd: connect/.test(message)
      ? `${usbmuxdHint} (${message})`
      : message,
  );
}

export async function driver(args, { input = undefined } = {}) {
  const running = exec(driverPath(), args, { maxBuffer: Infinity });
  if (input !== undefined) {
    running.child.stdin.end(input);
  }
  try {
    const { stdout } = await running;
    return stdout.trim() === "" ? undefined : JSON.parse(stdout);
  } catch (error) {
    if (error.code === "ENOENT") {
      throw new MissingDriverError();
    }
    throw driverError(error.stderr ?? "", error.message);
  }
}

const onDevice = (device, args, options = {}) =>
  driver(["device", "-d", device.udid, ...args], options);

const guest = (device, args, options = {}) =>
  driver(
    ["guest", "-d", device.udid, "--socket", device.socket, ...args],
    options,
  );

function callGuest(device, method, params = {}) {
  return new Promise<any>((resolve, reject) => {
    const socket = net.createConnection(device.socket, () =>
      socket.write(`${JSON.stringify({ t: "rpc", method, params })}\n`),
    );
    const chunks = [];
    socket.on("data", (chunk) => {
      chunks.push(chunk);
      if (chunk.includes(10)) {
        socket.destroy();
        try {
          const reply = JSON.parse(Buffer.concat(chunks) as unknown as string);
          if (reply.ok) {
            resolve(reply.result);
          } else {
            reject(new Error(`${method}: ${JSON.stringify(reply.error)}`));
          }
        } catch (error) {
          reject(error);
        }
      }
    });
    socket.on("error", reject);
    socket.on("close", () =>
      reject(new Error(`${method}: connection closed before reply`)),
    );
  });
}

const launchpadSocket = path.join(
  os.homedir(),
  "Library/Application Support/vphone-launchpad/control.sock",
);

async function guests() {
  if (!fs.existsSync(launchpadSocket)) {
    return [];
  }
  let stdout;
  try {
    ({ stdout } = await exec("vphone-launchpad-cli", ["vm", "list"]));
  } catch (error) {
    if (error.code === "ENOENT") {
      return [];
    }
    throw error;
  }
  return JSON.parse(stdout)
    .filter((machine) => machine.state === "running" && machine.udid)
    .map((machine) => ({
      name: machine.name,
      udid: machine.udid,
      socket: machine.controlSocket,
      virtual: true,
    }));
}

// vphone guests are built with chip ID 0xFE01, which their UDID starts with
// (vphone-cli 8c3630c7, Research/Guest/xcode_install_signature_gate.md).
const guestUDID = /^0000FE01-/;

const deviceLine = (device) => `  ${device.name}  ${device.udid}`;

interface DeviceQuery {
  required?: boolean;
  filter?: (devices: any[]) => Promise<any[]> | any[];
}

async function findDevice(context, { required = true, filter }: DeviceQuery) {
  const given = context.options.device;
  const [listed, virtual] = await step(
    tr("looking for devices"),
    () => Promise.all([driver(["device", "list"]), guests()]),
    { transient: true },
  );
  const devices = [
    ...listed
      .filter((device) => !guestUDID.test(device.udid))
      .map((device) => ({
        name: device.name || device.udid,
        udid: device.udid,
        paired: device.paired,
      })),
    ...virtual,
  ];
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
  const failure = new Error(
    `${tr("{reason}; connected iPhone and iPad devices:", { reason })}\n${devices.map(deviceLine).join("\n")}`,
  );
  if (matches.length === 0) {
    throw failure;
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
    failure,
  );
}

export const apps = async (device) =>
  device.virtual ? guestApps(device) : onDevice(device, ["apps"]);

export const lockedError = /device is locked/;

export const installApp = (device, app, id) =>
  onDevice(device, ["install", app, id]);

export const uninstallApp = async (device, id) =>
  device.virtual
    ? callGuest(device, "apps.uninstall", { bundle_id: id, force: true })
    : onDevice(device, ["uninstall", id]);

export const launchApp = async (device, id, args = []) =>
  device.virtual
    ? launchOnGuest(device, id)
    : onDevice(device, ["launch", id, "--", ...args]);

export const openSettings = (device, url) =>
  onDevice(device, ["launch", "--payload-url", url, "com.apple.Preferences"]);

export async function terminateApp(device, id) {
  if (device.virtual) {
    await callGuest(device, "apps.terminate", { bundle_id: id });
    return;
  }
  await onDevice(device, ["terminate", id]);
}

async function guestApps(device) {
  const { apps: listed } = await callGuest(device, "apps.list");
  return listed.map((app) => ({
    bundleIdentifier: app.bundle_id,
    version: app.version,
    bundleVersion: app.build,
  }));
}

async function launchOnGuest(device, id) {
  const until = Date.now() + 30_000;
  while (true) {
    try {
      await callGuest(device, "screen.unlock");
      await callGuest(device, "apps.launch", { bundle_id: id });
      return;
    } catch (error) {
      if (Date.now() > until) {
        throw error;
      }
    }
    await new Promise((resolve) => setTimeout(resolve, 1_000));
  }
}

export const configureGuest = (device, id, name) =>
  guest(device, ["configure", id, name]);

export async function coreDevice(context, options: DeviceQuery = {}) {
  const device = await findDevice(context, options);
  if (device.virtual) {
    await step(
      tr("pairing with {device}", { device: device.name }),
      () => guest(device, ["pair"]),
      { transient: true },
    );
  } else if (!device.paired) {
    await step(
      tr(
        "tap Trust on {device} and enter its passcode to trust this computer",
        {
          device: device.name,
        },
      ),
      () => onDevice(device, ["trust"]),
    );
  }
  return device;
}

const inContainer = (device, args, options = {}) =>
  (device.virtual ? guest : onDevice)(device, args, options);

export const containerHasFile = (device, id, file) =>
  inContainer(device, ["has", id, file]);

export const pushToContainer = (device, id, source, destination) =>
  inContainer(device, ["push", id, source, destination]);

export const containerSQL = (
  device,
  id,
  file,
  statements,
  { query = false } = {},
) =>
  inContainer(device, ["sqlite", id, file, ...(query ? ["--query"] : [])], {
    input: statements,
  });

export const provisioningProfiles = (device) => onDevice(device, ["profiles"]);
