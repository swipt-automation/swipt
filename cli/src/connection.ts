import dgram from "node:dgram";
import { on } from "node:events";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

import Bonjour from "bonjour-service";
import multicastDNS from "multicast-dns";

import { Code, RpcError, UsageError, open } from "./client";
import { tr } from "./i18n";
import { columns } from "./output";
import {
  DeviceConnectionProblem,
  DeviceConnectionState,
} from "./proto/common_pb.js";

const browseLimit = 1500;
const browseRequery = 300;
const publishLimit = 10_000;
const answerLimit = 5000;

const storeFile = path.join(
  process.env.XDG_CONFIG_HOME || path.join(os.homedir(), ".config"),
  "swipt",
  "devices.json",
);

function load() {
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

function save(devices) {
  fs.mkdirSync(path.dirname(storeFile), { recursive: true, mode: 0o700 });
  fs.writeFileSync(storeFile, `${JSON.stringify(devices, null, 2)}\n`, {
    mode: 0o600,
  });
}

export function label(name) {
  const slug = [...name.replaceAll(/['’]/g, "")]
    .map((character) => (/[A-Za-z0-9]/.test(character) ? character : "-"))
    .join("")
    .split("-")
    .filter((part) => part !== "")
    .join("-");
  return slug === "" ? name : slug;
}

export const sameName = (a, b) => a.toLowerCase() === b.toLowerCase();

const storedName = (stored, name) =>
  Object.keys(stored).find((it) => sameName(it, name));

function parseURL(text) {
  let parsed;
  try {
    parsed = new URL(text.includes("://") ? text : `http://${text}`);
  } catch {
    throw new UsageError(tr("{text} is not a URL", { text }));
  }
  if (
    parsed.hostname === "" ||
    (parsed.protocol !== "http:" && parsed.protocol !== "https:")
  ) {
    throw new UsageError(tr("{text} is not an http or https URL", { text }));
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

export const withToken = (url) => url.replace("://", "://TOKEN@");

async function browse(signal, until, limit = browseLimit) {
  const bonjour = new Bonjour({
    socket: dgram.createSocket("udp4"),
    bind: false,
  } as any);
  const found = new Map();
  const enough = new AbortController();
  try {
    const browser = bonjour.find({ type: "swipt" }, (service) => {
      if (!service.host || found.has(service.name)) {
        return;
      }
      const host = service.host.replace(/\.$/, "").toLowerCase();
      found.set(service.name, {
        name: label(service.name),
        url: `http://${host.includes(":") ? `[${host}]` : host}:${service.port}`,
        authenticates: service.txt?.auth === "1",
      });
      if (until?.([...found.values()])) {
        enough.abort();
      }
    });
    const requery = setInterval(() => browser.update(), browseRequery);
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
    browser.stop();
  } finally {
    bonjour.destroy();
  }
  return [...found.values()];
}

export async function findPublished(name, signal) {
  const wanted = label(name);
  const matches = (service) => sameName(service.name, wanted);
  const found = await browse(
    signal,
    (services) => services.some(matches),
    publishLimit,
  );
  return found.find(matches);
}

export async function runningSwipt(name, signal) {
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

export async function tryUnlock(client, signal) {
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

async function discover(signal, stored, until?) {
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
      if (current.token === "" && current.url !== it.url) {
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

const requiresToken = (found) =>
  tr("{device} requires a token: swipt device add {url}", {
    device: found.name,
    url: withToken(found.url),
  });

export async function attach(context) {
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
          tr('there is no device {device}; "swipt device list" lists them', {
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
    let found = [];
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
          'there is no device; turn on Swipt › Developer › Local Network, or connect one with "swipt device add URL"',
        ),
      );
    }
    if (names.length > 1) {
      throw new Error(
        tr("there is more than one device: {devices}; choose one with -d", {
          devices: names.join(", "),
        }),
      );
    }
    device = parseURL(stored[names[0]]);
  }
  context.target.url = device.url;
  return open(device);
}

const connectionStates = {
  [DeviceConnectionState.UNSPECIFIED]: tr("unspecified"),
  [DeviceConnectionState.STOPPED]: tr("stopped"),
  [DeviceConnectionState.CONNECTING]: tr("connecting"),
  [DeviceConnectionState.PREPARING]: tr("preparing"),
  [DeviceConnectionState.RUNNING]: tr("running"),
  [DeviceConnectionState.FAILED]: tr("failed"),
};

const connectionProblems = {
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
  [DeviceConnectionProblem.OTHER]: tr("other"),
};

function connectionText(connection) {
  let text = connectionStates[connection?.state ?? 0];
  if (
    connection &&
    connection.problem !== DeviceConnectionProblem.UNSPECIFIED
  ) {
    text += `, ${connectionProblems[connection.problem]}`;
  }
  if (connection?.message) {
    text += `: ${connection.message}`;
  }
  return text;
}

async function onLocalNetwork(host, signal) {
  const mdns = multicastDNS({
    socket: dgram.createSocket("udp4"),
    bind: false,
  });
  const query = () =>
    mdns.query([
      { name: host, type: "A" },
      { name: host, type: "AAAA" },
    ]);
  const requery = setInterval(query, browseRequery);
  query();
  try {
    for await (const [response] of on(mdns, "response", { signal })) {
      if (response.answers.some((it) => sameName(it.name, host))) {
        return true;
      }
    }
  } catch (error) {
    if (!signal.aborted) {
      throw error;
    }
  } finally {
    clearInterval(requery);
    mdns.destroy();
  }
  return false;
}

async function probe(url, signal) {
  try {
    const device = parseURL(url);
    const { hostname } = new URL(device.url);
    if (
      hostname.endsWith(".local") &&
      !(await onLocalNetwork(hostname, signal))
    ) {
      return { cells: [tr("offline")] };
    }
    const info = await open(device).system.getInfo({}, { signal });
    return {
      cells: [
        `${info.appVersion} (${info.appBuild})`,
        connectionText(info.deviceConnection),
      ],
    };
  } catch (error) {
    if (error instanceof RpcError && error.code === Code.Unauthenticated) {
      return { cells: [tr("requires a token")] };
    }
    return {
      cells: [tr("offline")],
      failure: signal.aborted ? undefined : error.message,
    };
  }
}

export async function list({ signal }) {
  const stored = load();
  const deadline = AbortSignal.any([signal, AbortSignal.timeout(browseLimit)]);
  const probes = new Map(
    Object.values(stored).map((url) => [url, probe(url, deadline)]),
  );
  const locked = (await discover(signal, stored)).filter(
    (it) => it.authenticates && storedName(stored, it.name) === undefined,
  );
  const names = Object.keys(stored).sort((a, b) => a.localeCompare(b));
  if (names.length === 0 && locked.length === 0) {
    console.error(
      tr(
        'no device publishes Swipt on the local network; turn on Swipt › Developer › Local Network, or connect one with "swipt device add URL"',
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

export async function add(context) {
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

export function remove({ args }) {
  const stored = load();
  const name = storedName(stored, label(args[0]));
  if (name === undefined) {
    throw new Error(tr("there is no device {device}", { device: args[0] }));
  }
  delete stored[name];
  save(stored);
  console.log(tr("removed {name}", { name }));
}
