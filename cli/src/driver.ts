import fs from "node:fs";
import path from "node:path";

import { findDriver, stopDriver } from "./connection";
import { appRunning, coreDevice, serve, terminateApp } from "./coredevice";
import { configDirectory } from "./directories";
import { tr } from "./i18n";
import { FalseResult, choose, columns, interactive, step } from "./output";
import { askPasscode, passcodeOf } from "./passcode";
import { savedPasscode, swiptApps } from "./sideload";

// Passcodes for unlocking each device automatically, by UDID; null when its
// user declined.
const passcodeFile = path.join(configDirectory, "passcodes.json");

function loadPasscodes() {
  try {
    return JSON.parse(fs.readFileSync(passcodeFile, "utf8"));
  } catch (error) {
    if (error.code === "ENOENT") {
      return {};
    }
    throw error;
  }
}

function savePasscode(udid, passcode) {
  const saved = loadPasscodes();
  saved[udid] = passcode ?? null;
  fs.mkdirSync(configDirectory, { recursive: true, mode: 0o700 });
  fs.writeFileSync(passcodeFile, `${JSON.stringify(saved, null, 2)}\n`, {
    mode: 0o600,
  });
}

async function confirm(context, message, action, failure) {
  const confirmed = await choose(
    context,
    message,
    [
      { name: action, value: true },
      { name: tr("Cancel"), value: false },
    ],
    failure,
  );
  if (!confirmed) {
    throw new FalseResult();
  }
}

async function runningApps(device) {
  const ids = await swiptApps(device);
  const running = await Promise.all(ids.map((id) => appRunning(device, id)));
  return { ids, running: ids.filter((_, index) => running[index]) };
}

// The device has one accessibility client: Swipt on the device, or a driver
// on one computer. Returns the IDs of Swipt on the device.
async function takeOver(context, device) {
  const [{ ids, running }, driving] = await step(
    tr("checking {device}", { device: device.name }),
    () =>
      Promise.all([
        runningApps(device),
        findDriver(device.udid, context.signal),
      ]),
    { transient: true },
  );
  if (driving !== undefined) {
    const parameters = {
      device: device.name,
      computer: driving.name,
      url: driving.url,
    };
    await confirm(
      context,
      tr(
        "The driver on {computer} serves {device}, and only one of them can control it. Stop that driver?",
        parameters,
      ),
      tr("Stop the driver on {computer}", parameters),
      new Error(
        tr(
          'The driver on {computer} serves {device}, and only one of them can control it; stop it with "swipt device stop -d {url}".',
          parameters,
        ),
      ),
    );
    await step(tr("stopping the driver on {computer}", parameters), () =>
      stopDriver(driving, context.signal),
    );
  }
  if (running.length > 0) {
    const parameters = { device: device.name };
    await confirm(
      context,
      tr(
        "Swipt is running on {device}, and only one of them can control it. Quit Swipt?",
        parameters,
      ),
      tr("Quit Swipt"),
      new Error(
        tr(
          "Swipt is running on {device}, and only one of them can control it; quit Swipt first.",
          parameters,
        ),
      ),
    );
    await step(tr("quitting Swipt on {device}", parameters), () =>
      Promise.all(running.map((id) => terminateApp(device, id))),
    );
  }
  return ids;
}

async function swiptPasscode(device, ids) {
  for (const id of ids) {
    try {
      const passcode = await savedPasscode(device, id);
      if (passcode !== undefined) {
        return passcode;
      }
    } catch {}
  }
  return undefined;
}

// The driver unlocks the device whenever it needs to, for as long as it runs.
async function unlockPasscode(context, device, ids) {
  const { options } = context;
  if (options.passcode !== undefined) {
    const passcode = passcodeOf(options);
    savePasscode(device.udid, passcode);
    return passcode;
  }
  const saved = loadPasscodes();
  if (device.udid in saved) {
    return saved[device.udid] ?? undefined;
  }
  let passcode = await step(
    tr("checking Swipt on {device}", { device: device.name }),
    () => swiptPasscode(device, ids),
    { transient: true },
  );
  if (passcode === undefined) {
    if (!interactive(context)) {
      return undefined;
    }
    passcode = await askPasscode(context);
  }
  savePasscode(device.udid, passcode);
  return passcode;
}

export async function run(context) {
  if (context.options["passcode-type"] !== undefined) {
    passcodeOf(context.options);
  }
  const device = await coreDevice(context, { required: false, virtual: false });
  const ids = await takeOver(context, device);
  const passcode = await unlockPasscode(context, device, ids);
  const driven = serve(device.udid, { passcode, signal: context.signal });
  const ready = await step(
    tr("starting the driver for {device}", { device: device.name }),
    () => driven.serving,
    { transient: true },
  );
  console.log(
    `${ready.device}\n${columns([
      ["CLI", ready.api],
      ["MCP", ready.mcp],
      [tr("Local network"), ready.network],
    ])}`,
  );
  console.error(
    passcode === undefined
      ? tr(
          "{device} isn't unlocked automatically; set its passcode with --passcode. Ctrl-C stops the driver.",
          { device: ready.device },
        )
      : tr("Ctrl-C stops the driver."),
  );
  driven.stderr.pipe(process.stderr);
  await driven.finished();
  context.signal.throwIfAborted();
}
