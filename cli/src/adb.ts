import { spawn } from "node:child_process";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

import Bonjour, { type Service } from "bonjour-service";

import { label, sameName } from "./connection";
import { type Application, randomToken } from "./driver";
import { t } from "./i18n";
import { multicastOptions, resolveHost } from "./mdns";
import { step } from "./output";
import type { Passcode } from "./passcode";
import { bundleId } from "./swipt-app";

const minimumAndroidSDK = 34;
const requiredAndroidABI = "arm64-v8a";
const mdnsBrowseTimeoutMs = 1500;
const mdnsBrowseRequeryMs = 300;
const mdnsConnectTimeoutMs = 2000;
const probeTimeoutMs = 3000;
const setupPromptDelayMs = 4000;
const setupTimeoutMs = 75_000;
const setupRequestFile = "/data/local/tmp/swipt-setup.json";
const setupResultFile = "/data/local/tmp/swipt-setup.done";

const identityProbe = `
avd=$(getprop ro.boot.qemu.avd_name)
if [ "$(getprop ro.boot.qemu)" = 1 ] && [ -n "$avd" ]; then
  echo "emulator:$avd"
  echo "$avd"
  echo 1
else
  getprop ro.serialno
  name=$(settings get global device_name 2>/dev/null)
  if [ -z "$name" ] || [ "$name" = null ]; then
    name=$(getprop ro.product.model)
  fi
  echo "$name"
  echo 0
fi
getprop ro.build.version.sdk
getprop ro.product.cpu.abilist64
`;

export interface AndroidDevice {
  platform: "android";
  name: string;
  id: string;
  serial: string;
  authorized: boolean;
  virtual: boolean;
  sdk: number;
  abi: string;
}

type PrivilegeTarget = "Shizuku" | "Root" | "None";

interface SetupResult {
  id: string;
  port: number;
  authenticates: boolean;
  error?: string;
}

let cachedAdb: string | null | undefined;

function adbPath() {
  if (cachedAdb !== undefined) {
    return cachedAdb ?? undefined;
  }
  const executable = process.platform === "win32" ? "adb.exe" : "adb";
  const pathDirs = (process.env.PATH ?? "")
    .split(path.delimiter)
    .filter(Boolean)
    .map((dir) => path.join(dir, executable));
  const sdkDirs = [
    process.env.ANDROID_HOME,
    process.env.ANDROID_SDK_ROOT,
    process.platform === "darwin"
      ? path.join(os.homedir(), "Library/Android/sdk")
      : process.platform === "linux"
        ? path.join(os.homedir(), "Android/Sdk")
        : process.platform === "win32" && process.env.LOCALAPPDATA
          ? path.join(process.env.LOCALAPPDATA, "Android/Sdk")
          : undefined,
  ]
    .filter((dir): dir is string => Boolean(dir))
    .map((dir) => path.join(dir, "platform-tools", executable));
  const posixDirs =
    process.platform === "win32"
      ? []
      : ["/opt/homebrew/bin/adb", "/usr/local/bin/adb"];
  const candidates = [
    ...(process.env.ADB ? [process.env.ADB] : []),
    ...pathDirs,
    ...sdkDirs,
    ...posixDirs,
  ];
  cachedAdb = candidates.find((file) => fs.existsSync(file)) ?? null;
  return cachedAdb ?? undefined;
}

export const adbAvailable = () => adbPath() !== undefined;

function runAdb(
  args: string[],
  {
    input,
    signal,
    timeoutMs,
  }: { input?: string; signal?: AbortSignal; timeoutMs?: number } = {},
) {
  const file = adbPath();
  if (file === undefined) {
    return Promise.reject(new Error(t("sideload.noTools")));
  }
  return new Promise<string>((resolve, reject) => {
    const child = spawn(file, args, {
      stdio: ["pipe", "pipe", "pipe"],
      windowsHide: true,
    });
    let stdout = "";
    let stderr = "";
    let timedOut = false;
    const stop = () => {
      child.kill();
    };
    const timer =
      timeoutMs === undefined
        ? undefined
        : setTimeout(() => {
            timedOut = true;
            stop();
          }, timeoutMs);
    if (signal?.aborted) {
      stop();
    } else {
      signal?.addEventListener("abort", stop, { once: true });
    }
    child.stdin.on("error", () => {});
    if (input !== undefined) {
      child.stdin.write(input);
    }
    child.stdin.end();
    child.stdout.setEncoding("utf8").on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.setEncoding("utf8").on("data", (chunk) => {
      stderr += chunk;
    });
    child.once("error", (error) => {
      clearTimeout(timer);
      signal?.removeEventListener("abort", stop);
      reject(error);
    });
    child.once("close", (code, signalName) => {
      clearTimeout(timer);
      signal?.removeEventListener("abort", stop);
      if (signal?.aborted) {
        reject(signal.reason);
      } else if (timedOut) {
        reject(new Error(`adb ${args[0] ?? ""} timed out`));
      } else if (code === 0) {
        resolve(stdout);
      } else {
        const detail = (stderr.trim() || stdout.trim()).replace(/^adb:\s*/, "");
        reject(
          new Error(
            detail || `adb exited with ${code ?? `signal ${signalName}`}`,
          ),
        );
      }
    });
  });
}

const onSerial = (
  serial: string,
  args: string[],
  options?: { input?: string; signal?: AbortSignal; timeoutMs?: number },
) => runAdb(["-s", serial, ...args], options);

interface RawAdbEntry {
  serial: string;
  state: "device" | "unauthorized";
  model?: string;
}

function parseDevicesOutput(output: string): RawAdbEntry[] {
  const entries: RawAdbEntry[] = [];
  for (const raw of output.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith("List of devices") || line.startsWith("*")) {
      continue;
    }
    const match = /^(\S+)\s+(device|unauthorized)(?:\s+(.*))?$/.exec(line);
    if (!match) {
      continue;
    }
    const [, serial, state, rest = ""] = match;
    const model = /(?:^|\s)model:(\S+)/.exec(rest)?.[1]?.replaceAll("_", " ");
    entries.push({
      serial,
      state: state as "device" | "unauthorized",
      model,
    });
  }
  return entries;
}

const connectedPattern = /^(?:connected|already connected) to\b/im;
const noRoutePattern = /\bNo route to host\b/i;

const matchesPreferred = (device: AndroidDevice, preferred: string) =>
  sameName(device.serial, preferred) ||
  sameName(device.id, preferred) ||
  sameName(label(device.name), label(preferred));

async function serviceEndpoint(service: Service, signal: AbortSignal) {
  if (!service.port) {
    return undefined;
  }
  const addresses = service.addresses ?? [];
  const ipv4 =
    addresses.find((it) => net.isIPv4(it)) ??
    (service.referer?.family === "IPv4" && net.isIPv4(service.referer.address)
      ? service.referer.address
      : undefined);
  const ipv6 = addresses.find((it) => net.isIPv6(it) && !/^fe[89ab]/i.test(it));
  const host = service.host?.replace(/\.$/, "");
  const address =
    ipv4 ?? ipv6 ?? (host ? await resolveHost(host, signal) : undefined);
  if (!address) {
    return undefined;
  }
  return net.isIPv6(address)
    ? `[${address}]:${service.port}`
    : `${address}:${service.port}`;
}

async function probeDevice(
  entry: RawAdbEntry,
  signal: AbortSignal,
): Promise<AndroidDevice | undefined> {
  if (entry.state === "unauthorized") {
    return {
      platform: "android",
      name: entry.model || entry.serial,
      id: entry.serial,
      serial: entry.serial,
      authorized: false,
      virtual: entry.serial.startsWith("emulator-"),
      sdk: 0,
      abi: "",
    };
  }
  try {
    const lines = (
      await onSerial(entry.serial, ["shell", identityProbe], {
        signal,
        timeoutMs: probeTimeoutMs,
      })
    )
      .replaceAll("\r", "")
      .split("\n");
    const [id = "", name = "", virtualFlag = "0", sdk = "0", abi = ""] = lines;
    return {
      platform: "android",
      name: name.trim() || entry.model || entry.serial,
      id: id.trim() || entry.serial,
      serial: entry.serial,
      authorized: true,
      virtual: virtualFlag.trim() === "1",
      sdk: Number.parseInt(sdk.trim(), 10) || 0,
      abi: abi.trim(),
    };
  } catch (error) {
    if (signal.aborted) {
      throw error;
    }
    if (entry.serial.includes(":")) {
      await runAdb(["disconnect", entry.serial], {
        signal,
        timeoutMs: mdnsConnectTimeoutMs,
      }).catch(() => {});
    }
    return undefined;
  }
}

function preferTransport(
  current: AndroidDevice,
  candidate: AndroidDevice,
  preferredSerial: string | undefined,
) {
  if (preferredSerial !== undefined) {
    if (sameName(candidate.serial, preferredSerial)) {
      return candidate;
    }
    if (sameName(current.serial, preferredSerial)) {
      return current;
    }
  }
  if (candidate.authorized !== current.authorized) {
    return candidate.authorized ? candidate : current;
  }
  const currentUSB = !current.serial.includes(":");
  const candidateUSB = !candidate.serial.includes(":");
  return candidateUSB && !currentUSB ? candidate : current;
}

async function discoverWirelessDevices({
  attached,
  emitted,
  canRestartServer,
  preferredSerial,
  virtual,
  signal,
  onDevice,
}: {
  attached: Set<string>;
  emitted: Set<string>;
  canRestartServer: boolean;
  preferredSerial: string | undefined;
  virtual: boolean;
  signal: AbortSignal;
  onDevice: (device: AndroidDevice) => void;
}) {
  const seenEndpoints = new Set<string>(attached);
  const connectingSerials = new Set<string>();
  const pending: Promise<void>[] = [];
  const enough = new AbortController();
  const browseSignal = AbortSignal.any([signal, enough.signal]);
  let restartPromise: Promise<void> | undefined;

  const connectEndpoint = async (endpoint: string) => {
    const attempt = () =>
      runAdb(["connect", endpoint], {
        signal,
        timeoutMs: mdnsConnectTimeoutMs,
      });
    let text: string;
    try {
      text = await attempt();
    } catch (error) {
      if (signal.aborted) {
        throw error;
      }
      text = error instanceof Error ? error.message : String(error);
    }
    if (connectedPattern.test(text)) {
      return true;
    }
    if (
      process.platform === "darwin" &&
      canRestartServer &&
      noRoutePattern.test(text)
    ) {
      restartPromise ??= runAdb(["kill-server"], {
        signal,
        timeoutMs: mdnsConnectTimeoutMs,
      }).then(
        () => {},
        () => {},
      );
      await restartPromise;
      try {
        return connectedPattern.test(await attempt());
      } catch {
        return false;
      }
    }
    return false;
  };

  const tryCandidate = (
    endpoint: string,
    hardwareSerial: string | undefined,
  ) => {
    if (
      seenEndpoints.has(endpoint) ||
      signal.aborted ||
      enough.signal.aborted
    ) {
      return;
    }
    const wantsEndpoint =
      preferredSerial !== undefined && sameName(endpoint, preferredSerial);
    if (
      !wantsEndpoint &&
      hardwareSerial !== undefined &&
      (attached.has(hardwareSerial) ||
        emitted.has(hardwareSerial) ||
        connectingSerials.has(hardwareSerial))
    ) {
      return;
    }
    seenEndpoints.add(endpoint);
    if (hardwareSerial !== undefined) {
      connectingSerials.add(hardwareSerial);
    }
    pending.push(
      (async () => {
        try {
          if (!(await connectEndpoint(endpoint))) {
            return;
          }
          const entries = parseDevicesOutput(
            await runAdb(["devices", "-l"], { signal }),
          );
          const entry = entries.find((it) => it.serial === endpoint) ?? {
            serial: endpoint,
            state: "device" as const,
          };
          const device = await probeDevice(entry, signal);
          if (
            device === undefined ||
            (!virtual && device.virtual) ||
            emitted.has(device.id)
          ) {
            return;
          }
          emitted.add(device.id);
          attached.add(device.serial);
          onDevice(device);
          if (
            preferredSerial !== undefined &&
            matchesPreferred(device, preferredSerial)
          ) {
            enough.abort();
          }
        } catch (error) {
          if (signal.aborted) {
            throw error;
          }
        } finally {
          if (hardwareSerial !== undefined) {
            connectingSerials.delete(hardwareSerial);
          }
        }
      })(),
    );
  };

  const responders = multicastOptions().map(
    (options) =>
      new Bonjour(options as ConstructorParameters<typeof Bonjour>[0]),
  );
  try {
    const browsers = responders.map((bonjour) =>
      bonjour.find({ type: "adb-tls-connect" }, (service) => {
        const txtSerial =
          typeof service.txt?.serial === "string"
            ? service.txt.serial.trim()
            : "";
        const hardwareSerial =
          txtSerial || /^adb-([^-]+)-/.exec(service.name)?.[1];
        pending.push(
          serviceEndpoint(service, browseSignal).then(
            (endpoint) => {
              if (endpoint !== undefined) {
                tryCandidate(endpoint, hardwareSerial);
              }
            },
            () => {},
          ),
        );
      }),
    );
    pending.push(
      runAdb(["mdns", "services"], {
        signal: browseSignal,
        timeoutMs: mdnsBrowseTimeoutMs,
      }).then(
        (output) => {
          for (const raw of output.split(/\r?\n/)) {
            const match = /^(\S+)\s+_adb-tls-connect\._tcp\.?\s+(\S+)$/.exec(
              raw.trim(),
            );
            if (!match) {
              continue;
            }
            const [, instance, endpoint] = match;
            tryCandidate(endpoint, /^adb-([^-]+)-/.exec(instance)?.[1]);
          }
        },
        () => {},
      ),
    );
    const requery = setInterval(() => {
      for (const browser of browsers) {
        browser.update();
      }
    }, mdnsBrowseRequeryMs);
    try {
      await sleep(mdnsBrowseTimeoutMs, undefined, { signal: browseSignal });
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
  let length = 0;
  while (pending.length > length) {
    const slice = pending.slice(length);
    length = pending.length;
    await Promise.all(slice);
  }
}

export async function listAndroidDevices({
  preferredSerial,
  virtual = true,
  signal,
  onDevice,
}: {
  preferredSerial?: string;
  virtual?: boolean;
  signal: AbortSignal;
  onDevice: (device: AndroidDevice) => void;
}) {
  if (!adbAvailable()) {
    return;
  }
  const entries = parseDevicesOutput(
    await runAdb(["devices", "-l"], { signal }),
  );
  const probed = (
    await Promise.all(entries.map((entry) => probeDevice(entry, signal)))
  ).filter((device): device is AndroidDevice => device !== undefined);
  const attached = new Set(probed.map((it) => it.serial));
  const byId = new Map<string, AndroidDevice>();
  for (const device of probed) {
    if (!virtual && device.virtual) {
      continue;
    }
    const existing = byId.get(device.id);
    byId.set(
      device.id,
      existing === undefined
        ? device
        : preferTransport(existing, device, preferredSerial),
    );
  }
  const emitted = new Set<string>();
  for (const device of byId.values()) {
    emitted.add(device.id);
    onDevice(device);
  }
  if (
    signal.aborted ||
    (preferredSerial !== undefined &&
      [...byId.values()].some((it) => matchesPreferred(it, preferredSerial)))
  ) {
    return;
  }
  await discoverWirelessDevices({
    attached,
    emitted,
    canRestartServer: probed.length === 0,
    preferredSerial,
    virtual,
    signal,
    onDevice,
  });
}

export async function authorizeAndroidDevice(
  device: AndroidDevice,
  signal: AbortSignal,
): Promise<AndroidDevice> {
  if (device.authorized) {
    return device;
  }
  await step(t("sideload.allowDebugging", { device: device.name }), () =>
    onSerial(device.serial, ["wait-for-device"], { signal }),
  );
  const authorized = await probeDevice(
    { serial: device.serial, state: "device", model: device.name },
    signal,
  );
  if (authorized === undefined) {
    throw new Error(t("sideload.deviceNotFound", { name: device.name }));
  }
  return authorized;
}

export function checkAndroidSupported(device: AndroidDevice) {
  const abis = device.abi
    .split(",")
    .map((it) => it.trim())
    .filter(Boolean);
  if (device.sdk < minimumAndroidSDK || !abis.includes(requiredAndroidABI)) {
    throw new Error(
      t("sideload.androidUnsupported", {
        device: device.name,
        sdk: String(device.sdk),
        abi: device.abi || "32-bit",
      }),
    );
  }
}

export async function androidSwiptApp(
  device: AndroidDevice,
  signal?: AbortSignal,
): Promise<Application | undefined> {
  if (!device.authorized) {
    return undefined;
  }
  let dump: string;
  try {
    dump = await onSerial(
      device.serial,
      [
        "shell",
        `pm path ${bundleId} >/dev/null 2>&1 && dumpsys package ${bundleId}`,
      ],
      { signal },
    );
  } catch (error) {
    if (signal?.aborted) {
      throw error;
    }
    return undefined;
  }
  const versionCode = /\bversionCode=(\d+)\b/.exec(dump)?.[1];
  const versionName = /\bversionName=(\S+)/.exec(dump)?.[1];
  if (versionCode === undefined || versionName === undefined) {
    return undefined;
  }
  return {
    bundleIdentifier: bundleId,
    name: "Swipt",
    version: versionName,
    bundleVersion: versionCode,
  };
}

export async function installAndroidAPK(
  device: AndroidDevice,
  apk: string,
  {
    downgrade = false,
    signal,
  }: { downgrade?: boolean; signal?: AbortSignal } = {},
) {
  try {
    await onSerial(
      device.serial,
      ["install", "-r", ...(downgrade ? ["-d"] : []), apk],
      { signal },
    );
  } catch (error) {
    if (
      error instanceof Error &&
      error.message.includes("INSTALL_FAILED_UPDATE_INCOMPATIBLE")
    ) {
      throw new Error(
        t("sideload.differentSignature", { device: device.name }),
      );
    }
    throw error;
  }
}

export async function uninstallAndroidApp(
  device: AndroidDevice,
  id: string,
  signal?: AbortSignal,
) {
  await onSerial(device.serial, ["uninstall", id], { signal });
  await onSerial(
    device.serial,
    [
      "shell",
      `pkill -f '^${id.replaceAll(".", "\\.")}:core$'; rm -rf /data/local/tmp/swipt ${setupRequestFile} ${setupResultFile}`,
    ],
    { signal },
  ).catch(() => {});
}

export async function launchAndroidApp(
  device: AndroidDevice,
  signal?: AbortSignal,
) {
  await onSerial(
    device.serial,
    [
      "shell",
      `input keyevent KEYCODE_WAKEUP >/dev/null 2>&1; wm dismiss-keyguard >/dev/null 2>&1; am start -W -n ${bundleId}/.app.MainActivity >/dev/null`,
    ],
    { signal },
  );
}

const preparePrivilegeScript = ({ virtual }: { virtual: boolean }) => `
rm -f ${setupResultFile} ${setupRequestFile}
pid=$(pidof shizuku_server 2>/dev/null | awk '{print $1}')
if [ -n "$pid" ]; then
  uid=$(stat -c %u "/proc/$pid" 2>/dev/null)
  if [ "$uid" = 0 ]; then
    echo Root
  else
    echo Shizuku
  fi
  exit 0
fi
${
  virtual
    ? `if command -v su >/dev/null 2>&1; then
  echo Root
  exit 0
fi`
    : ""
}
apk=$(pm path moe.shizuku.privileged.api 2>/dev/null | sed -n 's/^package://p' | head -n 1)
if [ -n "$apk" ]; then
  starter="$(dirname "$apk")/lib/arm64/libshizuku.so"
  if [ -x "$starter" ]; then
    "$starter" >/dev/null 2>&1 </dev/null &
    for _ in 1 2 3 4 5 6 7 8 9 10; do
      if pidof shizuku_server >/dev/null 2>&1; then
        echo Shizuku
        exit 0
      fi
      sleep 0.1
    done
  fi
fi
if command -v su >/dev/null 2>&1; then
  echo Root
elif [ -n "$apk" ]; then
  echo Shizuku
else
  echo None
fi
`;

export async function setupAndroidApp(
  device: AndroidDevice,
  {
    passcode,
    publishLocalNetwork,
    signal,
    onWaitingPrivilege,
  }: {
    passcode?: Passcode;
    publishLocalNetwork: boolean;
    signal: AbortSignal;
    onWaitingPrivilege: (mode: "Shizuku" | "Root") => void;
  },
): Promise<{ ready: boolean; port?: number; authenticates?: boolean }> {
  const detected = (
    await onSerial(
      device.serial,
      ["shell", preparePrivilegeScript({ virtual: device.virtual })],
      { signal },
    )
  )
    .trim()
    .split(/\s+/)
    .pop() as PrivilegeTarget | undefined;
  const mode: PrivilegeTarget =
    detected === "Root" || detected === "Shizuku" ? detected : "None";
  const token = randomToken();
  const payload = JSON.stringify({
    id: token,
    ...(passcode !== undefined
      ? { unlockType: passcode.type, unlockPasscode: passcode.passcode }
      : device.virtual
        ? { unlockType: "none", unlockIfUnset: true }
        : {}),
    localNetwork: publishLocalNetwork,
  });
  await onSerial(
    device.serial,
    ["shell", `umask 077 && cat > ${setupRequestFile}`],
    { input: payload, signal },
  );
  const privilegeArg =
    mode === "None" ? "" : ` --es io.nekohasekai.swipt.PRIVILEGE ${mode}`;
  await onSerial(
    device.serial,
    [
      "shell",
      `input keyevent KEYCODE_WAKEUP >/dev/null 2>&1; wm dismiss-keyguard >/dev/null 2>&1; am start -W -n ${bundleId}/.app.MainActivity --es io.nekohasekai.swipt.SETUP ${token}${privilegeArg} >/dev/null`,
    ],
    { signal },
  );
  if (mode === "None") {
    return { ready: false };
  }
  const promptTimer = setTimeout(() => {
    onWaitingPrivilege(mode);
  }, setupPromptDelayMs);
  try {
    const deadline = Date.now() + setupTimeoutMs;
    while (Date.now() < deadline) {
      const remainingSeconds = Math.max(
        1,
        Math.ceil((deadline - Date.now()) / 1000),
      );
      const output = await onSerial(
        device.serial,
        [
          "shell",
          `for _ in $(seq 1 ${remainingSeconds * 10}); do if [ -f ${setupResultFile} ]; then cat ${setupResultFile}; rm -f ${setupResultFile}; exit 0; fi; sleep 0.1; done; exit 1`,
        ],
        { signal, timeoutMs: remainingSeconds * 1000 + 2000 },
      );
      const result = JSON.parse(output.trim()) as SetupResult;
      if (result.id !== token) {
        continue;
      }
      if (result.error) {
        throw new Error(result.error);
      }
      return {
        ready: true,
        port: result.port,
        authenticates: result.authenticates,
      };
    }
    throw new Error(
      mode === "Root"
        ? t("sideload.allowRoot", { device: device.name })
        : t("sideload.allowShizuku", { device: device.name }),
    );
  } finally {
    clearTimeout(promptTimer);
  }
}

export async function forwardAndroidPort(
  device: AndroidDevice,
  port: number,
  signal?: AbortSignal,
) {
  await onSerial(device.serial, ["forward", `tcp:${port}`, `tcp:${port}`], {
    signal,
  });
}
