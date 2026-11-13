import fs from "node:fs";
import path from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

import Bonjour from "bonjour-service";

import type { Context } from "./cli/command";
import { type Client, Code, RpcError, UsageError, open } from "./client";
import { configDirectory } from "./directories";
import { tr } from "./i18n";
import { multicastOptions } from "./mdns";
import { columns, errorMessage } from "./output";
import {
  type DeviceConnection,
  DeviceConnectionProblem,
  DeviceConnectionState,
} from "./proto/common_pb";

const browseLimit = 1500;
const browseRequery = 300;
const publishLimit = 10_000;
const answerLimit = 5000;
const stopLimit = 10_000;

const storeFile = path.join(configDirectory, "devices.json");

type Devices = Record<string, string>;

function load(): Devices {
  let text;
  try {
    text = fs.readFileSync(storeFile, "utf8");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") {
      return {};
    }
    throw error;
  }
  return JSON.parse(text);
}

function save(devices: Devices) {
  fs.mkdirSync(path.dirname(storeFile), { recursive: true, mode: 0o700 });
  fs.writeFileSync(storeFile, `${JSON.stringify(devices, null, 2)}\n`, {
    mode: 0o600,
  });
}

export function label(name: string) {
  const slug = [...name.replaceAll(/['’]/g, "")]
    .map((character) => (/[A-Za-z0-9]/.test(character) ? character : "-"))
    .join("")
    .split("-")
    .filter((part) => part !== "")
    .join("-");
  return slug === "" ? name : slug;
}

export const sameName = (a: string, b: string) =>
  a.toLowerCase() === b.toLowerCase();

const storedName = (stored: Devices, name: string) =>
  Object.keys(stored).find((it) => sameName(it, name));

function parseURL(text: string) {
  let parsed: URL;
  try {
    parsed = new URL(text.includes("://") ? text : `http://${text}`);
  } catch {
    throw new UsageError(tr("{text} isn't a URL.", { text }));
  }
  if (
    parsed.hostname === "" ||
    (parsed.protocol !== "http:" && parsed.protocol !== "https:")
  ) {
    throw new UsageError(tr("{text} isn't an http or https URL.", { text }));
  }
  let token;
  try {
    token = decodeURIComponent(parsed.username);
  } catch {
    token = parsed.username;
  }
  return {
    url: `${parsed.protocol}//${parsed.host}${parsed.pathname.replace(/\/+$/, "")}`,
    token,
  };
}

export const withToken = (url: string, token = "TOKEN") =>
  url.replace("://", `://${encodeURIComponent(token)}@`);

export interface Published {
  name: string;
  url: string;
  authenticates: boolean;
  udid?: string;
}

type Enough = (services: Published[]) => boolean;

async function browse(
  signal: AbortSignal,
  until?: Enough,
  limit = browseLimit,
) {
  // bonjour-service hands its options to multicast-dns as they are.
  const responders = multicastOptions().map(
    (options) =>
      new Bonjour(options as ConstructorParameters<typeof Bonjour>[0]),
  );
  const found = new Map<string, Published>();
  const enough = new AbortController();
  try {
    const browsers = responders.map((bonjour) =>
      bonjour.find({ type: "swipt" }, (service) => {
        if (!service.host || found.has(service.name)) {
          return;
        }
        const host = service.host.replace(/\.$/, "").toLowerCase();
        found.set(service.name, {
          name: label(service.name),
          url: `http://${host.includes(":") ? `[${host}]` : host}:${service.port}`,
          authenticates: service.txt?.auth === "1",
          udid: service.txt?.udid,
        });
        if (until?.([...found.values()])) {
          enough.abort();
        }
      }),
    );
    const requery = setInterval(() => {
      for (const browser of browsers) {
        browser.update();
      }
    }, browseRequery);
    try {
      await sleep(limit, undefined, {
        signal: AbortSignal.any([signal, enough.signal]),
      });
    } catch (error) {
      if (signal.aborted) {
        throw error;
      }
    } finally {
      clearInterval(requery);
    }
    for (const browser of browsers) {
      browser.stop();
    }
  } finally {
    for (const bonjour of responders) {
      bonjour.destroy();
    }
  }
  return [...found.values()];
}

export async function findPublished(name: string, signal: AbortSignal) {
  const wanted = label(name);
  const matches = (service: Published) => sameName(service.name, wanted);
  const found = await browse(
    signal,
    (services) => services.some(matches),
    publishLimit,
  );
  return found.find(matches);
}

export async function findDriver(udid: string, signal: AbortSignal) {
  const serves = (service: Published) =>
    service.udid !== undefined && sameName(service.udid, udid);
  return (await browse(signal, (services) => services.some(serves))).find(
    serves,
  );
}

export async function stopDriver(
  url: string,
  udid: string,
  signal: AbortSignal,
) {
  await open({ url, token: "" }).system.shutdown({}, { signal });
  const deadline = AbortSignal.any([signal, AbortSignal.timeout(stopLimit)]);
  try {
    while ((await findDriver(udid, deadline)) !== undefined) {
      await sleep(browseRequery, undefined, { signal: deadline });
    }
  } catch (error) {
    if (signal.aborted) {
      throw error;
    }
    throw new Error(
      tr("The driver on {computer} didn't stop.", {
        computer: new URL(url).hostname,
      }),
    );
  }
}

export async function runningSwipt(name: string, signal: AbortSignal) {
  const wanted = label(name);
  const stored = load();
  await discover(signal, stored, (services) =>
    services.some((it) => sameName(it.name, wanted)),
  );
  const known = storedName(stored, wanted);
  if (known === undefined) {
    return undefined;
  }
  const client = open(parseURL(stored[known]));
  try {
    await client.system.getInfo(
      {},
      { signal: AbortSignal.any([signal, AbortSignal.timeout(answerLimit)]) },
    );
  } catch (error) {
    if (signal.aborted || !(error instanceof RpcError)) {
      throw error;
    }
    if (error.code === Code.Unauthenticated) {
      console.error(tr("error: {message}", { message: error.message }));
    }
    return undefined;
  }
  return client;
}

export async function tryUnlock(client: Client, signal: AbortSignal) {
  try {
    for await (const response of client.repl.execute(
      { code: "await device.unlock()", args: [], program: false },
      { signal },
    )) {
      if (response.part.case === "error") {
        return response.part.value.message;
      }
    }
  } catch (error) {
    if (signal.aborted || !(error instanceof RpcError)) {
      throw error;
    }
    return error.message;
  }
  return undefined;
}

async function discover(signal: AbortSignal, stored: Devices, until?: Enough) {
  const found = await browse(signal, until);
  let changed = false;
  for (const it of found) {
    if (it.authenticates) {
      continue;
    }
    const name = storedName(stored, it.name);
    if (name === undefined) {
      stored[it.name] = it.url;
      changed = true;
    } else {
      const current = parseURL(stored[name]);
      if (
        current.token === "" &&
        current.url !== it.url &&
        new URL(current.url).hostname.endsWith(".local")
      ) {
        stored[name] = it.url;
        changed = true;
      }
    }
  }
  if (changed) {
    save(stored);
  }
  return found;
}

const requiresToken = (found: Published) =>
  tr("{device} requires a token: swipt device add {url}", {
    device: found.name,
    url: withToken(found.url),
  });

export async function attach(
  context: Pick<Context<{ device?: string }>, "options" | "signal" | "target">,
) {
  const given = context.options.device;
  const stored = load();
  let device;
  if (given && /[:/]/.test(given)) {
    device = parseURL(given);
  } else if (given) {
    const name = label(given);
    let known = storedName(stored, name);
    if (known === undefined) {
      const found = (
        await discover(context.signal, stored, (services) =>
          services.some((it) => sameName(it.name, name)),
        )
      ).find((it) => sameName(it.name, name));
      if (!found) {
        throw new Error(
          tr('There is no device {device}; "swipt device list" lists them.', {
            device: name,
          }),
        );
      }
      if (found.authenticates) {
        throw new Error(requiresToken(found));
      }
      known = found.name;
    }
    device = parseURL(stored[known]);
  } else {
    let found: Published[] = [];
    if (Object.keys(stored).length === 0) {
      found = await discover(context.signal, stored);
    }
    const names = Object.keys(stored);
    if (names.length === 0) {
      const locked = found.filter((it) => it.authenticates);
      if (locked.length > 0) {
        throw new Error(locked.map(requiresToken).join("\n"));
      }
      throw new Error(
        tr(
          'There is no device; turn on Swipt › Developer › Local Network, or connect one with "swipt device add URL".',
        ),
      );
    }
    if (names.length > 1) {
      throw new Error(
        tr("There is more than one device: {devices}; choose one with -d", {
          devices: names.join(", "),
        }),
      );
    }
    device = parseURL(stored[names[0]]);
  }
  context.target.url = device.url;
  return open(device);
}

const connectionStates: Partial<Record<DeviceConnectionState, string>> = {
  [DeviceConnectionState.STOPPED]: tr("stopped", {}, "device connection"),
  [DeviceConnectionState.CONNECTING]: tr("connecting", {}, "device connection"),
  [DeviceConnectionState.PREPARING]: tr("preparing", {}, "device connection"),
  [DeviceConnectionState.RUNNING]: tr("running", {}, "device connection"),
  [DeviceConnectionState.FAILED]: tr("failed", {}, "device connection"),
};

const connectionProblems: Partial<Record<DeviceConnectionProblem, string>> = {
  [DeviceConnectionProblem.UNREACHABLE]: tr("unreachable"),
  [DeviceConnectionProblem.PAIRING_REJECTED]: tr("pairing rejected"),
  [DeviceConnectionProblem.DEVELOPER_MODE_OFF]: tr("developer mode off"),
  [DeviceConnectionProblem.DISK_IMAGE_MISSING]: tr("disk image missing"),
  [DeviceConnectionProblem.DISK_IMAGE_MOUNT_FAILED]: tr(
    "disk image mount failed",
  ),
  [DeviceConnectionProblem.DEVICE_UNSUPPORTED]: tr("device unsupported"),
  [DeviceConnectionProblem.SCREEN_IN_USE]: tr("screen in use"),
  [DeviceConnectionProblem.VPN_PEER_ADDRESS_MISSING]: tr(
    "VPN peer address missing",
  ),
};

function connectionText(connection: DeviceConnection | undefined) {
  if (connection === undefined) {
    return "";
  }
  return [
    [connectionStates[connection.state], connectionProblems[connection.problem]]
      .filter(Boolean)
      .join(", "),
    connection.message,
  ]
    .filter(Boolean)
    .join(": ");
}

async function probe(
  url: string,
  signal: AbortSignal,
): Promise<{ cells: string[]; failure?: string }> {
  try {
    const info = await open(parseURL(url)).system.getInfo({}, { signal });
    return {
      cells: [
        info.appBuild === ""
          ? info.appVersion
          : `${info.appVersion} (${info.appBuild})`,
        connectionText(info.deviceConnection),
      ],
    };
  } catch (error) {
    if (error instanceof RpcError && error.code === Code.Unauthenticated) {
      return { cells: [tr("requires a token")] };
    }
    return {
      cells: [tr("offline")],
      failure: signal.aborted ? undefined : errorMessage(error),
    };
  }
}

export async function list({ signal }: Pick<Context, "signal">) {
  const stored = load();
  const deadline = AbortSignal.any([signal, AbortSignal.timeout(browseLimit)]);
  const probes = new Map(
    Object.values(stored).map((url) => [url, probe(url, deadline)]),
  );
  const locked = (await discover(signal, stored)).filter(
    (it) =>
      it.authenticates &&
      !Object.values(stored).some((url) => parseURL(url).url === it.url),
  );
  const names = Object.keys(stored).sort((a, b) => a.localeCompare(b));
  if (names.length === 0 && locked.length === 0) {
    console.error(
      tr(
        'No device publishes Swipt on the local network; turn on Swipt › Developer › Local Network, or connect one with "swipt device add URL".',
      ),
    );
    return;
  }
  const late = AbortSignal.any([signal, AbortSignal.timeout(browseLimit)]);
  const probed = await Promise.all(
    names.map(async (name) => ({
      name,
      ...(await (probes.get(stored[name]) ?? probe(stored[name], late))),
    })),
  );
  console.log(
    columns(
      [
        ...probed.map(({ name, cells }) => [
          name,
          parseURL(stored[name]).url,
          ...cells,
        ]),
        ...locked.map((it) => [it.name, it.url, tr("requires a token")]),
      ].sort((a, b) => a[0].localeCompare(b[0])),
    ),
  );
  for (const { name, failure } of probed) {
    if (failure !== undefined) {
      console.error(`${name}: ${failure}`);
    }
  }
  for (const it of locked) {
    console.error(requiresToken(it));
  }
}

export async function add(
  context: Pick<Context, "args" | "signal" | "target">,
) {
  const device = parseURL(context.args[0]);
  context.target.url = device.url;
  const info = await open(device).system.getInfo(
    {},
    { signal: context.signal },
  );
  const name = label(info.deviceName) || new URL(device.url).hostname;
  const stored = load();
  const previous = storedName(stored, name);
  if (previous !== undefined) {
    delete stored[previous];
  }
  stored[name] = context.args[0];
  save(stored);
  console.log(tr("added {device}", { device: name }));
}

export function remove({ args }: Pick<Context, "args">) {
  const stored = load();
  const name = storedName(stored, label(args[0]));
  if (name === undefined) {
    throw new Error(tr("There is no device {device}.", { device: args[0] }));
  }
  delete stored[name];
  save(stored);
  console.log(tr("removed {name}", { name }, "device"));
}
