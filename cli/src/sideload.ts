import {
  type ExecFileException,
  type StdioOptions,
  execFile,
  execFileSync,
} from "node:child_process";
import { X509Certificate, createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import { promisify } from "node:util";

import select from "@inquirer/select";

import {
  type AndroidDevice,
  adbAvailable,
  androidSwiptApp,
  authorizeAndroidDevice,
  checkAndroidSupported,
  forwardAndroidPort,
  installAndroidAPK,
  launchAndroidApp,
  listAndroidDevices,
  setupAndroidApp,
  uninstallAndroidApp,
} from "./adb";
import type { Context } from "./cli/command";
import { UsageError, open } from "./client";
import {
  findPublished,
  label,
  runningSwipt,
  sameName,
  tryUnlock,
  withToken,
} from "./connection";
import { cacheDirectory } from "./directories";
import {
  DriverError,
  type Application,
  type Device,
  apps,
  configureVirtual,
  driverAvailable,
  failures,
  installApp,
  launchApp,
  listIOSDevices,
  mountDiskImage,
  openSettings,
  provisioningProfiles,
  randomToken,
  serve,
  terminateApp,
  trustIOSDevice,
  uninstallApp,
  waitForDevice,
} from "./driver";
import { language, t } from "./i18n";
import {
  type Choice,
  type PromptContext,
  choose,
  columns,
  errorMessage,
  interactive,
  printScriptError,
  selectStream,
  selectTheme,
  step,
} from "./output";
import {
  type Passcode,
  type PasscodeOptions,
  askPasscode,
  passcodeOf,
  validatePasscodeType,
} from "./passcode";
import { LogLevel, type ScriptError } from "./proto/common_pb";
import type { ConsoleOutput } from "./proto/repl_pb";
import {
  type SwiptApp,
  bundleId,
  bundleTeam,
  hasPairing,
  publishArgument,
  savePairing,
  savePreferences,
  savedPasscode,
  setUpAutoUnlock,
  setupArgument,
  swiptApps,
  teamPattern,
} from "./swipt-app";

export type IOSDevice = Device & { platform: "ios" };
export type TargetDevice = IOSDevice | AndroidDevice;

interface SetupOptions {
  "no-setup"?: boolean;
  "no-setup-pair"?: boolean;
  "no-setup-api"?: boolean;
}

type SetupContext = Pick<Context<SetupOptions>, "options" | "signal">;

type PasscodeSetupContext = PromptContext &
  Pick<Context<PasscodeOptions & SetupOptions>, "options">;

type DeployContext = PasscodeSetupContext &
  Pick<Context<{ nightly?: boolean; force?: boolean }>, "options">;

interface Period {
  creation: number;
  expiration: number;
}

interface Team {
  id: string;
  name?: string;
}

interface Signing {
  identity: string;
  entitlements: string;
  profile?: string;
}

interface GitHubAsset {
  id: number;
  name: string;
  url: string;
}

interface GitHubRelease {
  tag_name: string;
  assets: GitHubAsset[];
}

interface Release {
  tag: string;
  version: string;
  build: number;
  asset: GitHubAsset;
}

interface TargetFilter {
  label: string;
  empty: string;
  match: (
    device: TargetDevice,
    signal: AbortSignal,
  ) => Promise<boolean> | boolean;
}

interface TargetQuery {
  required?: boolean;
  virtual?: boolean;
  checkSupport?: boolean;
  filter?: TargetFilter;
}

interface InstalledApps {
  ios: Map<string, SwiptApp[]>;
  android: Map<string, Application | undefined>;
}

class GitHubError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

const exec = promisify(execFile);

const repository = "swipt-automation/swipt";
const autoSelectGrace = 50;

const guestTeam = "SWIPTLOCAL";
// installd on a vphone guest accepts an ad hoc signature and needs
// application-identifier (vphone-cli 8c3630c7,
// VPhoneGuestComponents/MISFix/MISFixInstallPolicy.c).
const guestEntitlements = (
  id: string,
) => `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>application-identifier</key>
	<string>${guestTeam}.${id}</string>
	<key>com.apple.developer.team-identifier</key>
	<string>${guestTeam}</string>
	<key>com.apple.security.application-groups</key>
	<array>
		<string>${guestTeam}.${id}</string>
	</array>
	<key>get-task-allow</key>
	<true/>
	<key>keychain-access-groups</key>
	<array>
		<string>${guestTeam}.${id}</string>
	</array>
</dict>
</plist>
`;

const accountsHint = "Xcode › Settings › Accounts";
const xcodeProfiles = "Library/Developer/Xcode/UserData/Provisioning Profiles";
const downloads = path.join(cacheDirectory, "sideload");

const signingNeedsMac = t("sideload.needsMacOS");

const expiresSoon = ({ creation, expiration }: Period) =>
  expiration - Date.now() < (expiration - creation) / 2;

const plistValueMissing = (error: unknown) =>
  error instanceof Error &&
  "stderr" in error &&
  typeof error.stderr === "string" &&
  /No value at that key path/.test(error.stderr);

const failedWith = (error: unknown, code: string): error is DriverError =>
  error instanceof DriverError && error.code === code;

function headers(accept: string) {
  const token = process.env.GITHUB_TOKEN;
  return {
    Accept: accept,
    "X-GitHub-Api-Version": "2022-11-28",
    "User-Agent": "swipt-cli",
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

async function github(url: string, accept: string, signal: AbortSignal) {
  let response = await fetch(url, {
    headers: headers(accept),
    signal,
    redirect: "manual",
  });
  const location = response.headers.get("location");
  if (response.status >= 300 && response.status < 400 && location !== null) {
    response = await fetch(location, { signal });
  }
  if (!response.ok) {
    let message = `GitHub answered ${response.status} for ${url}`;
    if ([401, 403, 429].includes(response.status)) {
      message += `; ${process.env.GITHUB_TOKEN ? "GITHUB_TOKEN" : "set GITHUB_TOKEN to a token that"} must read the repository and stay under the rate limit`;
    }
    throw new GitHubError(message, response.status);
  }
  return response;
}

async function findRelease(
  nightly: boolean | undefined,
  signal: AbortSignal,
  platform: "ios" | "android",
): Promise<Release> {
  const read = async (name: string) =>
    (await (
      await github(
        `https://api.github.com/repos/${repository}/releases/${name}`,
        "application/vnd.github+json",
        signal,
      )
    ).json()) as GitHubRelease;
  const release = nightly
    ? await read("tags/nightly")
    : await read("latest").catch((error: unknown) => {
        if (!(error instanceof GitHubError) || error.status !== 404) {
          throw error;
        }
        return read("tags/nightly");
      });
  const asset = (name: string) => {
    const found = release.assets.find((it) => it.name === name);
    if (!found) {
      throw new Error(`release ${release.tag_name} has no ${name}`);
    }
    return found;
  };
  const metadata = (await (
    await github(asset("metadata.json").url, "application/octet-stream", signal)
  ).json()) as { version: string; build: number };
  return {
    tag: release.tag_name,
    version: metadata.version,
    build: metadata.build,
    asset: asset(platform === "android" ? "Swipt.apk" : "Swipt.ipa"),
  };
}

async function download(
  url: string,
  file: string,
  signal: AbortSignal,
  update: (text: string) => void,
) {
  const response = await github(url, "application/octet-stream", signal);
  if (response.body === null) {
    throw new Error(
      `GitHub answered ${response.status} without a body for ${url}`,
    );
  }
  const total = Number(response.headers.get("content-length"));
  const megabytes = (bytes: number) => (bytes / 1_000_000).toFixed(1);
  let received = 0;
  await pipeline(
    Readable.fromWeb(response.body),
    async function* (source: AsyncIterable<Uint8Array>) {
      for await (const chunk of source) {
        received += chunk.length;
        update(
          total > 0
            ? `${megabytes(received)} / ${megabytes(total)} MB  ${Math.floor((received / total) * 100)}%`
            : `${megabytes(received)} MB`,
        );
        yield chunk;
      }
    },
    fs.createWriteStream(file),
    { signal },
  );
}

async function unpack(ipa: string, directory: string) {
  await exec("ditto", ["-x", "-k", ipa, directory]);
  const payload = path.join(directory, "Payload");
  const app = fs.readdirSync(payload).find((name) => name.endsWith(".app"));
  if (app === undefined) {
    throw new Error("the IPA has no app in Payload");
  }
  return path.join(payload, app);
}

const pbxproj = (id: string, team: string) => `// !$*UTF8*$!
{
	archiveVersion = 1;
	classes = {
	};
	objectVersion = 56;
	objects = {
		A1 = {isa = PBXFileReference; lastKnownFileType = sourcecode.c.c; path = main.c; sourceTree = "<group>"; };
		A2 = {isa = PBXFileReference; explicitFileType = wrapper.application; includeInIndex = 0; path = Stub.app; sourceTree = BUILT_PRODUCTS_DIR; };
		A3 = {isa = PBXBuildFile; fileRef = A1; };
		B1 = {isa = PBXGroup; children = (B2, B3); sourceTree = "<group>"; };
		B2 = {isa = PBXGroup; children = (A1); path = Stub; sourceTree = "<group>"; };
		B3 = {isa = PBXGroup; children = (A2); name = Products; sourceTree = "<group>"; };
		C1 = {isa = PBXSourcesBuildPhase; buildActionMask = 2147483647; files = (A3); runOnlyForDeploymentPostprocessing = 0; };
		D1 = {
			isa = PBXNativeTarget;
			buildConfigurationList = E1;
			buildPhases = (C1);
			buildRules = ();
			dependencies = ();
			name = Stub;
			productName = Stub;
			productReference = A2;
			productType = "com.apple.product-type.application";
		};
		E1 = {isa = XCConfigurationList; buildConfigurations = (F1); defaultConfigurationIsVisible = 0; defaultConfigurationName = Release; };
		E2 = {isa = XCConfigurationList; buildConfigurations = (F2); defaultConfigurationIsVisible = 0; defaultConfigurationName = Release; };
		F1 = {
			isa = XCBuildConfiguration;
			buildSettings = {
				CODE_SIGN_STYLE = Automatic;
				DEVELOPMENT_TEAM = "${team}";
				GENERATE_INFOPLIST_FILE = YES;
				IPHONEOS_DEPLOYMENT_TARGET = 16.4;
				PRODUCT_BUNDLE_IDENTIFIER = "${id}";
				PRODUCT_NAME = Stub;
				SDKROOT = iphoneos;
				TARGETED_DEVICE_FAMILY = "1,2";
			};
			name = Release;
		};
		F2 = {isa = XCBuildConfiguration; buildSettings = {}; name = Release; };
		G1 = {
			isa = PBXProject;
			buildConfigurationList = E2;
			compatibilityVersion = "Xcode 14.0";
			mainGroup = B1;
			productRefGroup = B3;
			projectDirPath = "";
			projectRoot = "";
			targets = (D1);
		};
	};
	rootObject = G1;
}
`;

function explain(output: string, team: string) {
  const errors = [
    ...new Set(
      output
        .split("\n")
        .filter((line) => /^(xcodebuild: )?error: /.test(line))
        .map((line) => line.replace(/^(xcodebuild: )?error: /, "")),
    ),
  ].join("; ");
  if (/No Accounts?\b/.test(output)) {
    return t("sideload.noAccountForTeam", { team, accounts: accountsHint });
  }
  if (/signing certificate|certificateType/i.test(output)) {
    return t("sideload.certificateFailed", {
      team,
      accounts: accountsHint,
      errors,
    });
  }
  if (/requires Xcode|xcode-select/.test(output)) {
    return t("sideload.xcodeMissing");
  }
  return errors || output.trim();
}

function profileField(file: string, key: string) {
  return execFileSync("plutil", ["-extract", key, "raw", "-o", "-", "-"], {
    input: execFileSync("security", ["cms", "-D", "-i", file], {
      stdio: ["ignore", "pipe", "ignore"],
    }),
    encoding: "utf8",
  }).trim();
}

function revoked(certificates: string) {
  const [leaf, issuer] = [0, 1].map(
    (index) => new X509Certificate(fs.readFileSync(`${certificates}${index}`)),
  );
  const url = /^OCSP - URI:(.+)$/m.exec(leaf.infoAccess ?? "")?.[1];
  if (url === undefined) {
    return false;
  }
  fs.writeFileSync(`${certificates}0.pem`, leaf.toString());
  fs.writeFileSync(`${certificates}1.pem`, issuer.toString());
  try {
    return /: revoked$/m.test(
      execFileSync(
        "openssl",
        [
          "ocsp",
          "-issuer",
          `${certificates}1.pem`,
          "-cert",
          `${certificates}0.pem`,
          "-url",
          url,
          "-noverify",
        ],
        {
          encoding: "utf8",
          stdio: ["ignore", "pipe", "ignore"],
          timeout: 10_000,
        },
      ),
    );
  } catch {
    return false;
  }
}

async function provision(
  work: string,
  id: string,
  team: string,
  device: Device,
  update: (text: string) => void,
): Promise<Signing> {
  const stub = path.join(work, "Stub");
  const products = path.join(stub, "Products");
  const app = path.join(products, "Stub.app");
  const profile = path.join(app, "embedded.mobileprovision");
  fs.mkdirSync(path.join(stub, "Stub.xcodeproj"), { recursive: true });
  fs.mkdirSync(path.join(stub, "Stub"));
  fs.writeFileSync(
    path.join(stub, "Stub/main.c"),
    "int main(void) { return 0; }\n",
  );
  fs.writeFileSync(
    path.join(stub, "Stub.xcodeproj/project.pbxproj"),
    pbxproj(id, team),
  );
  const build = async () => {
    const running = exec(
      "xcodebuild",
      [
        "build",
        "-project",
        path.join(stub, "Stub.xcodeproj"),
        "-scheme",
        "Stub",
        "-configuration",
        "Release",
        "-destination",
        `id=${device.udid}`,
        `CONFIGURATION_BUILD_DIR=${products}`,
        `OBJROOT=${path.join(stub, "Objects")}`,
        "-allowProvisioningUpdates",
        "-allowProvisioningDeviceRegistration",
      ],
      { maxBuffer: Infinity },
    );
    running.child.stdout?.on("data", (chunk: Buffer) => {
      const line = String(chunk)
        .split("\n")
        .findLast((it) => /^[A-Z]\w* /.test(it));
      if (line) {
        update(line.split(" ")[0]);
      }
    });
    try {
      await running;
    } catch (error) {
      const failure = error as ExecFileException & {
        stdout: string;
        stderr: string;
      };
      if (failure.code === "ENOENT") {
        throw new Error(t("sideload.xcodebuildMissing"));
      }
      if (typeof failure.code !== "number") {
        throw error;
      }
      throw new Error(explain(`${failure.stdout}${failure.stderr}`, team));
    }
  };
  const certificates = path.join(work, "certificate");
  const stdio: StdioOptions = ["ignore", "pipe", "ignore"];
  const extract = () =>
    execFileSync(
      "codesign",
      ["-d", `--extract-certificates=${certificates}`, app],
      { stdio },
    );
  await build();
  extract();
  if (
    expiresSoon({
      creation: Date.parse(profileField(profile, "CreationDate")),
      expiration: Date.parse(profileField(profile, "ExpirationDate")),
    }) ||
    revoked(certificates)
  ) {
    fs.rmSync(
      path.join(
        os.homedir(),
        xcodeProfiles,
        `${profileField(profile, "UUID")}.mobileprovision`,
      ),
      { force: true },
    );
    await build();
    extract();
  }
  const entitlements = path.join(work, "entitlements.plist");
  fs.writeFileSync(
    entitlements,
    execFileSync("codesign", ["-d", "--entitlements", "-", "--xml", app], {
      stdio,
    }),
  );
  return {
    identity: createHash("sha1")
      .update(fs.readFileSync(`${certificates}0`))
      .digest("hex")
      .toUpperCase(),
    entitlements,
    profile,
  };
}

async function resign(
  app: string,
  id: string,
  signing: Signing,
  update: (text: string) => void,
) {
  const info = path.join(app, "Info.plist");
  execFileSync("plutil", [
    "-replace",
    "CFBundleIdentifier",
    "-string",
    id,
    info,
  ]);
  let permitted: string[] | undefined;
  try {
    permitted = JSON.parse(
      execFileSync(
        "plutil",
        [
          "-extract",
          "BGTaskSchedulerPermittedIdentifiers",
          "json",
          "-o",
          "-",
          info,
        ],
        { stdio: "pipe", encoding: "utf8" },
      ),
    );
  } catch (error) {
    if (!plistValueMissing(error)) {
      throw error;
    }
  }
  if (permitted !== undefined) {
    execFileSync("plutil", [
      "-replace",
      "BGTaskSchedulerPermittedIdentifiers",
      "-json",
      JSON.stringify(
        permitted.map((it) =>
          it.startsWith(`${bundleId}.`) ? id + it.slice(bundleId.length) : it,
        ),
      ),
      info,
    ]);
  }
  const profile = path.join(app, "embedded.mobileprovision");
  if (signing.profile === undefined) {
    fs.rmSync(profile, { force: true });
  } else {
    fs.copyFileSync(signing.profile, profile);
  }
  const codesign = ["--force", "--sign", signing.identity, "--timestamp=none"];
  const frameworks = path.join(app, "Frameworks");
  const nested = [
    ...fs
      .readdirSync(app)
      .filter((name) => name.endsWith(".dylib"))
      .map((name) => path.join(app, name)),
    ...(fs.existsSync(frameworks)
      ? fs.readdirSync(frameworks).map((name) => path.join(frameworks, name))
      : []),
  ];
  for (const file of nested) {
    update(path.basename(file));
    await exec("codesign", [...codesign, file]);
  }
  update(path.basename(app));
  await exec("codesign", [
    ...codesign,
    "--entitlements",
    signing.entitlements,
    app,
  ]);
}

function xcodeTeams() {
  const valid = new Set(
    [
      ...execFileSync(
        "security",
        ["find-identity", "-v", "-p", "codesigning"],
        {
          encoding: "utf8",
        },
      ).matchAll(/\) ([0-9A-F]{40}) "Apple Development: /g),
    ].map((match) => match[1]),
  );
  const certificates = execFileSync(
    "security",
    ["find-certificate", "-a", "-Z", "-p", "-c", "Apple Development"],
    { encoding: "utf8" },
  ).matchAll(
    /SHA-1 hash: ([0-9A-F]{40})[\s\S]*?(-----BEGIN CERTIFICATE-----[\s\S]*?-----END CERTIFICATE-----)/g,
  );
  const teams = new Map<string, string | undefined>();
  for (const [, hash, pem] of certificates) {
    if (!valid.has(hash)) {
      continue;
    }
    const subject = new X509Certificate(pem).subject;
    const id = /^OU=(.+)$/m.exec(subject)?.[1];
    if (id !== undefined && teamPattern.test(id)) {
      teams.set(id, /^O=(.+)$/m.exec(subject)?.[1].replaceAll("\\", ""));
    }
  }
  return [...teams].map(([id, name]): Team => ({ id, name }));
}

function xcodeSignedIn() {
  try {
    const lists: Record<string, unknown[]> = JSON.parse(
      execFileSync(
        "plutil",
        [
          "-extract",
          "DVTDeveloperAccountManagerAppleIDLists",
          "json",
          "-o",
          "-",
          "-",
        ],
        {
          input: execFileSync("defaults", [
            "export",
            "com.apple.dt.Xcode",
            "-",
          ]),
          stdio: "pipe",
          encoding: "utf8",
        },
      ),
    );
    return Object.values(lists).some((accounts) => accounts.length > 0);
  } catch (error) {
    if (plistValueMissing(error)) {
      return false;
    }
    throw error;
  }
}

const teamList = (teams: Team[]) =>
  teams
    .map((team) => `  ${team.id}${team.name ? `  ${team.name}` : ""}`)
    .join("\n");

const teamChoice = ({ id, name }: Team) => ({
  name: name ? `${name}  ${id}` : id,
  short: name ?? id,
  value: id,
});

const targetKey = (device: TargetDevice) =>
  device.platform === "ios" ? `ios:${device.udid}` : `android:${device.id}`;

const targetIdentifier = (device: TargetDevice) =>
  device.platform === "ios" ? device.udid : device.serial;

const targetLine = (device: TargetDevice) =>
  `  ${device.name}  ${targetIdentifier(device)}`;

const targetChoice = (device: TargetDevice): Choice<TargetDevice> => ({
  key: targetKey(device),
  name: `${device.name}  ${targetIdentifier(device)}`,
  short: device.name,
  value: device,
});

const matchesIdentifier = (device: TargetDevice, given: string) =>
  device.platform === "ios"
    ? sameName(device.udid, given)
    : sameName(device.serial, given) || sameName(device.id, given);

async function findTargetDevice(
  context: PromptContext,
  { required = true, virtual = true, filter }: TargetQuery,
): Promise<TargetDevice> {
  if (
    context.options.team !== undefined &&
    !teamPattern.test(context.options.team)
  ) {
    throw new UsageError(
      t("sideload.invalidTeam", {
        value: JSON.stringify(context.options.team),
        accounts: accountsHint,
      }),
    );
  }
  const given = context.options.device;
  const includeAndroid =
    context.options.team === undefined || given !== undefined;
  const hasIOS = driverAvailable();
  const hasAndroid = includeAndroid && adbAvailable();
  if (!hasIOS && !hasAndroid) {
    throw new Error(t("sideload.noTools"));
  }

  const search = new AbortController();
  const signal = AbortSignal.any([context.signal, search.signal]);
  if (given !== undefined || !interactive(context)) {
    const devices: TargetDevice[] = [];
    let iosFailure: unknown;
    try {
      await step(
        t("driver.lookingForDevices"),
        async () => {
          await Promise.all([
            hasIOS
              ? listIOSDevices({
                  virtual,
                  signal,
                  onDevice(device) {
                    devices.push(device);
                    if (
                      given !== undefined &&
                      matchesIdentifier(device, given)
                    ) {
                      search.abort();
                    }
                  },
                }).catch((error) => {
                  if (!search.signal.aborted) {
                    iosFailure = error;
                  }
                })
              : Promise.resolve(),
            hasAndroid
              ? listAndroidDevices({
                  preferredSerial: given,
                  virtual,
                  signal,
                  onDevice(device) {
                    devices.push(device);
                    if (
                      given !== undefined &&
                      matchesIdentifier(device, given)
                    ) {
                      search.abort();
                    }
                  },
                }).catch((error) => {
                  if (context.signal.aborted) {
                    throw error;
                  }
                })
              : Promise.resolve(),
          ]);
          context.signal.throwIfAborted();
        },
        { transient: true },
      );
    } finally {
      search.abort();
    }
    const exact = given
      ? devices.find((device) => matchesIdentifier(device, given))
      : undefined;
    if (exact !== undefined) {
      return exact;
    }
    const matches = given
      ? devices.filter((device) => sameName(label(device.name), label(given)))
      : devices;
    if ((given || !required) && matches.length === 1) {
      return matches[0];
    }
    if (devices.length === 0) {
      if (iosFailure !== undefined && !hasAndroid) {
        throw iosFailure;
      }
      throw new Error(t("sideload.noDevice"));
    }
    const reason = !given
      ? t("sideload.chooseDeviceHint")
      : matches.length === 0
        ? t("sideload.deviceNotFound", { name: given })
        : t("sideload.ambiguousDevice", { name: given });
    throw new Error(
      `${t("sideload.connectedDevices", { reason })}\n${devices.map(targetLine).join("\n")}`,
    );
  }

  const devices: TargetDevice[] = [];
  const choices: Choice<TargetDevice>[] = [];
  const listeners = new Set<() => void>();
  let remainingSources = Number(hasIOS) + Number(hasAndroid);
  let pending = 0;
  let failure: unknown;
  const notify = () => {
    for (const listener of listeners) {
      listener();
    }
  };
  const onDevice = (device: TargetDevice) => {
    devices.push(device);
    if (filter === undefined) {
      choices.push(targetChoice(device));
      notify();
      return;
    }
    pending++;
    notify();
    Promise.resolve(filter.match(device, signal))
      .catch(() => !signal.aborted)
      .then((matched) => {
        pending--;
        if (!signal.aborted && matched) {
          choices.push(targetChoice(device));
        }
        notify();
      });
  };
  const finishSource = (error?: unknown) => {
    remainingSources--;
    if (error !== undefined && !search.signal.aborted && !hasAndroid) {
      failure = error;
    }
    notify();
  };
  if (hasIOS) {
    listIOSDevices({ virtual, signal, onDevice }).then(
      () => finishSource(),
      (error) => finishSource(error),
    );
  }
  if (hasAndroid) {
    listAndroidDevices({ virtual, signal, onDevice }).then(
      () => finishSource(),
      () => finishSource(),
    );
  }

  const waitFor = (ready: () => boolean) =>
    new Promise<void>((resolve, reject) => {
      const cleanup = () => {
        listeners.delete(check);
        context.signal.removeEventListener("abort", check);
      };
      const check = () => {
        if (context.signal.aborted) {
          cleanup();
          reject(context.signal.reason);
        } else if (
          failure !== undefined &&
          remainingSources === 0 &&
          choices.length === 0
        ) {
          cleanup();
          reject(failure);
        } else if (ready()) {
          cleanup();
          resolve();
        }
      };
      listeners.add(check);
      context.signal.addEventListener("abort", check, { once: true });
      check();
    });

  const settleAutoSelect = async () => {
    const listing = remainingSources > 0;
    if (required || choices.length !== 1 || (!listing && pending === 0)) {
      return;
    }
    let elapsed = false;
    const timer = setTimeout(() => {
      elapsed = true;
      notify();
    }, autoSelectGrace);
    try {
      await waitFor(
        () =>
          elapsed ||
          choices.length > 1 ||
          (remainingSources === 0 && pending === 0),
      );
    } finally {
      clearTimeout(timer);
    }
  };

  try {
    await step(
      t("driver.lookingForDevices"),
      async () => {
        await waitFor(() => choices.length > 0 || remainingSources === 0);
        await settleAutoSelect();
      },
      { transient: true },
    );
    if (choices.length === 0 && pending > 0 && filter !== undefined) {
      await step(
        filter.label,
        async () => {
          await waitFor(() => choices.length > 0 || pending === 0);
          await settleAutoSelect();
        },
        { transient: true },
      );
    }
    if (choices.length === 0) {
      throw new Error(
        devices.length === 0 || filter === undefined
          ? t("sideload.noDevice")
          : filter.empty,
      );
    }
    if (
      !required &&
      remainingSources === 0 &&
      pending === 0 &&
      choices.length === 1
    ) {
      return choices[0].value;
    }
    return await selectStream(context, t("driver.chooseDevice"), {
      current: () => ({
        choices: [...choices],
        loading:
          remainingSources > 0
            ? t("driver.lookingForDevices")
            : pending > 0
              ? filter?.label
              : undefined,
        autoSelect: !required,
      }),
      subscribe: (listener) => {
        listeners.add(listener);
        return () => listeners.delete(listener);
      },
    });
  } finally {
    search.abort();
  }
}

export async function targetDevice(
  context: PromptContext,
  query: TargetQuery = {},
): Promise<TargetDevice> {
  const device = await findTargetDevice(context, query);
  if (device.platform === "android") {
    if (context.options.team !== undefined) {
      throw new UsageError(t("sideload.teamIOSOnly"));
    }
    const authorized = await authorizeAndroidDevice(device, context.signal);
    if (query.checkSupport !== false) {
      checkAndroidSupported(authorized);
    }
    return authorized;
  }
  return trustIOSDevice(device, context.signal);
}

function sideloadedFilter(
  installed: InstalledApps,
  empty: string,
): TargetFilter {
  return {
    label: t("sideload.looking"),
    empty,
    match: (device, signal) => {
      if (device.platform === "android") {
        if (!device.authorized) {
          return true;
        }
        return androidSwiptApp(device, signal).then(
          (app) => {
            installed.android.set(device.id, app);
            return app !== undefined;
          },
          () => true,
        );
      }
      return swiptApps(device, signal).then(
        (found) => {
          installed.ios.set(device.udid, found);
          return found.length > 0;
        },
        () => true,
      );
    },
  };
}

async function locateIOS(
  context: PromptContext,
  device: IOSDevice,
  {
    fresh = false,
    installed,
  }: { fresh?: boolean; installed?: SwiptApp[] } = {},
) {
  const { options } = context;
  const found =
    installed ??
    (await step(
      t("sideload.checking", { device: device.name }),
      () => swiptApps(device, context.signal),
      { transient: true },
    ));
  if (device.virtual) {
    return {
      current: found.find((app) => app.bundleIdentifier === bundleId),
    };
  }
  if (options.team !== undefined) {
    if (!teamPattern.test(options.team)) {
      throw new UsageError(
        t("sideload.invalidTeam", {
          value: JSON.stringify(options.team),
          accounts: accountsHint,
        }),
      );
    }
    return {
      team: options.team,
      current: found.find((app) => app.team === options.team),
    };
  }
  if (found.length > 1) {
    const known = process.platform === "darwin" ? xcodeTeams() : [];
    const teams = found.map((app) => ({
      id: app.team,
      name: known.find((team) => team.id === app.team)?.name,
    }));
    const team = await choose(
      context,
      t("sideload.chooseTeam"),
      teams.map(teamChoice),
      new Error(
        `${t("sideload.ambiguousTeam", { device: device.name })}\n${teamList(teams)}`,
      ),
    );
    return { team, current: found.find((app) => app.team === team) };
  }
  if (found.length === 0 && fresh) {
    if (process.platform !== "darwin") {
      throw new Error(signingNeedsMac);
    }
    const teams = xcodeTeams();
    if (teams.length === 0) {
      throw new Error(
        xcodeSignedIn()
          ? t("sideload.noCertificate", { accounts: accountsHint })
          : t("sideload.noAccount", { accounts: accountsHint }),
      );
    }
    const team = teams.some((it) => it.id === bundleTeam)
      ? bundleTeam
      : teams.length === 1
        ? teams[0].id
        : await choose(
            context,
            t("sideload.chooseTeam"),
            teams.map(teamChoice),
            new Error(`${t("sideload.chooseTeamHint")}\n${teamList(teams)}`),
          );
    return { team, current: undefined };
  }
  return { team: found[0]?.team, current: found[0] };
}

async function locateAndroid(
  context: Pick<PromptContext, "signal">,
  device: AndroidDevice,
  installed?: Map<string, Application | undefined>,
) {
  if (installed?.has(device.id)) {
    return installed.get(device.id);
  }
  return step(
    t("sideload.checking", { device: device.name }),
    () => androidSwiptApp(device, context.signal),
    { transient: true },
  );
}

const buildText = (app: Application) =>
  `Swipt ${app.version} (${app.bundleVersion})`;

async function signingPeriod(device: Device, id: string, team: string) {
  const names = [id, "*"].map((it) => `iOS Team Provisioning Profile: ${it}`);
  const profiles = await step(
    t("sideload.checking", { device: device.name }),
    () => provisioningProfiles(device),
    { transient: true },
  );
  return profiles
    .filter(
      (profile) =>
        profile.teamIdentifier === team && names.includes(profile.name),
    )
    .map((profile) => ({
      creation: Date.parse(profile.creationDate),
      expiration: Date.parse(profile.expirationDate),
    }))
    .reduce<Period>(
      (latest, it) => (it.expiration > latest.expiration ? it : latest),
      { creation: 0, expiration: 0 },
    );
}

function expirationText(time: number) {
  const hours = (time - Date.now()) / 3_600_000;
  const days = Math.round(hours / 24);
  const [value, unit]: [number, Intl.RelativeTimeFormatUnit] =
    hours < 24
      ? [Math.max(Math.round(hours), 1), "hour"]
      : days < 7
        ? [days, "day"]
        : days < 30
          ? [Math.round(days / 7), "week"]
          : days < 365
            ? [Math.round(days / 30), "month"]
            : [Math.round(days / 365), "year"];
  return new Intl.RelativeTimeFormat(language)
    .format(value, unit)
    .replace(/(\d)(\p{Script=Han})/gu, "$1 $2");
}

export async function installPasscode(
  context: PasscodeSetupContext,
  device: Pick<TargetDevice, "platform" | "virtual">,
  current: Application | undefined,
) {
  const platform = device.platform ?? "ios";
  return current === undefined &&
    !device.virtual &&
    interactive(context) &&
    context.options.passcode === undefined &&
    context.options["passcode-type"] === undefined &&
    !context.options["no-setup"]
    ? askPasscode(context, platform)
    : passcodeOf(context.options, platform);
}

async function findInstalled(context: PromptContext, checkSupport = true) {
  const installed: InstalledApps = {
    ios: new Map(),
    android: new Map(),
  };
  const device = await targetDevice(context, {
    checkSupport,
    filter: sideloadedFilter(installed, t("sideload.noneInstalledHint")),
  });
  if (device.platform === "android") {
    const current = await locateAndroid(context, device, installed.android);
    if (current === undefined) {
      throw new Error(t("sideload.notInstalledHint", { device: device.name }));
    }
    return { device, current };
  }
  const { team, current } = await locateIOS(context, device, {
    installed: installed.ios.get(device.udid),
  });
  if (current === undefined) {
    throw new Error(
      team === undefined
        ? t("sideload.notInstalledHint", { device: device.name })
        : t("sideload.notInstalledByTeamHint", { device: device.name, team }),
    );
  }
  return { device, team, current };
}

async function downloadReleaseAsset(
  context: Pick<Context, "signal">,
  release: Release,
  extension: "ipa" | "apk",
) {
  const name = `Swipt-${release.asset.id}.${extension}`;
  const target = path.join(downloads, name);
  if (fs.existsSync(target)) {
    return target;
  }
  fs.mkdirSync(downloads, { recursive: true });
  const partial = `${target}.download`;
  await step(
    t("sideload.downloading", {
      version: release.version,
      build: release.build,
      release: release.tag,
    }),
    (update) => download(release.asset.url, partial, context.signal, update),
  );
  fs.renameSync(partial, target);
  for (const entry of fs.readdirSync(downloads)) {
    if (entry !== name && entry.endsWith(`.${extension}`)) {
      fs.rmSync(path.join(downloads, entry), { force: true });
    }
  }
  return target;
}

async function prepareDiskImage(
  context: Pick<Context, "signal">,
  device: Device,
) {
  if (device.virtual) {
    return;
  }
  try {
    await step(
      t("sideload.preparingDiskImage", {
        device: device.name,
      }),
      () => mountDiskImage(device),
      { transient: true },
    );
  } catch (error) {
    if (context.signal.aborted) {
      throw error;
    }
    console.error(
      t("sideload.diskImageFailed", {
        device: device.name,
        error: errorMessage(error),
      }),
    );
  }
}

async function confirmDowngrade(
  context: DeployContext,
  deviceName: string,
  current: Application,
  release: Release,
  updateOnly: boolean,
) {
  const installed = Number(current.bundleVersion);
  const parameters = {
    build: buildText(current),
    device: deviceName,
    release: release.tag,
    releaseBuild: release.build,
  };
  const asks =
    !updateOnly &&
    !context.options.force &&
    installed > release.build &&
    interactive(context);
  if (installed >= release.build && !asks) {
    console.log(
      installed === release.build
        ? t("sideload.upToDate", parameters)
        : t("sideload.laterThanRelease", parameters),
    );
  }
  const versions = {
    device: deviceName,
    version: `${current.version} (${current.bundleVersion})`,
    releaseVersion: `${release.version} (${release.build})`,
  };
  const replaces = asks
    ? await select(
        {
          message:
            release.tag === "nightly"
              ? t("sideload.downgradePrompt", versions)
              : t("sideload.downgradeToStablePrompt", versions),
          theme: selectTheme,
          choices: [
            { name: t("sideload.downgrade"), value: true },
            { name: t("noThanks"), value: false },
          ],
        },
        { output: process.stderr, signal: context.signal },
      )
    : !updateOnly && Boolean(context.options.force);
  return { installed, parameters, replaces };
}

async function deployAndroid(
  context: DeployContext,
  device: AndroidDevice,
  current: Application | undefined,
  updateOnly: boolean,
) {
  const passcode = await installPasscode(context, device, current);
  const release = await step(
    t("sideload.checkingRelease"),
    () => findRelease(context.options.nightly, context.signal, "android"),
    { transient: true },
  );
  if (current !== undefined) {
    const { installed, replaces } = await confirmDowngrade(
      context,
      device.name,
      current,
      release,
      updateOnly,
    );
    if (!replaces && installed >= release.build) {
      const saves = !updateOnly && passcode !== undefined;
      if (!saves) {
        return;
      }
      if (context.options["no-setup"]) {
        await step(t("sideload.opening", { device: device.name }), () =>
          launchAndroidApp(device, context.signal),
        );
        return;
      }
      await configureAndOpenAndroid(context, device, passcode);
      return;
    }
  }
  const apk = await downloadReleaseAsset(context, release, "apk");
  await installAndOpen(
    context,
    { device, id: bundleId, current },
    apk,
    release.build,
    {
      passcode,
      saves: !updateOnly,
    },
  );
}

async function deployIOS(
  context: DeployContext,
  {
    device,
    team,
    current,
  }: { device: IOSDevice; team?: string; current: Application | undefined },
  updateOnly: boolean,
) {
  const passcode = await installPasscode(context, device, current);
  const release = await step(
    t("sideload.checkingRelease"),
    () => findRelease(context.options.nightly, context.signal, "ios"),
    { transient: true },
  );
  const id =
    team === undefined || team === bundleTeam
      ? bundleId
      : `${bundleId}.${team}`;
  if (current !== undefined) {
    const signing =
      team === undefined ? undefined : await signingPeriod(device, id, team);
    const { installed, parameters, replaces } = await confirmDowngrade(
      context,
      device.name,
      current,
      release,
      updateOnly,
    );
    if (
      !replaces &&
      installed >= release.build &&
      (signing === undefined || !expiresSoon(signing))
    ) {
      if (signing !== undefined) {
        console.log(
          t("sideload.expiration", {
            expiration: expirationText(signing.expiration),
          }),
        );
      }
      const saves = !updateOnly && passcode !== undefined;
      const pairs =
        !context.options["no-setup"] &&
        !context.options["no-setup-pair"] &&
        !(await step(
          t("sideload.checking", { device: device.name }),
          () => hasPairing(device, id),
          { transient: true },
        ));
      if (!saves && !pairs) {
        return;
      }
      await prepareDiskImage(context, device);
      if (saves) {
        await step(t("sideload.settingUpAutoUnlock"), () =>
          setUpAutoUnlock(device, id, passcode),
        );
      }
      if (context.options["no-setup"]) {
        await launch(device, id);
        return;
      }
      const paired = pairs && (await pair(context, device, id, true));
      if (saves || paired) {
        await openConfigured(context, device, id, passcode);
      }
      return;
    }
    if (!replaces && installed > release.build) {
      throw new Error(t("sideload.renewalNeedsDowngrade", parameters));
    }
  }
  if (team !== undefined && process.platform !== "darwin") {
    throw new Error(signingNeedsMac);
  }
  const work = fs.mkdtempSync(path.join(os.tmpdir(), "swipt-sideload-"));
  try {
    const signing =
      team === undefined
        ? undefined
        : await step(
            t("sideload.preparingSigning", {
              team: xcodeTeams().find((it) => it.id === team)?.name ?? team,
            }),
            (update) => provision(work, id, team, device, update),
          );
    const ipa = await downloadReleaseAsset(context, release, "ipa");
    const app =
      signing === undefined
        ? await unpack(ipa, work)
        : await step(t("sideload.resigning"), async (update) => {
            const unpacked = await unpack(ipa, work);
            await resign(unpacked, id, signing, update);
            return unpacked;
          });
    await installAndOpen(context, { device, id, current }, app, release.build, {
      passcode,
      saves: !updateOnly,
    });
  } finally {
    fs.rmSync(work, { recursive: true, force: true });
  }
}

async function installOnGuest(device: Device, id: string, app: string) {
  const work = fs.mkdtempSync(path.join(os.tmpdir(), "swipt-guest-"));
  try {
    const signed = path.join(work, "Payload", path.basename(app));
    await exec("ditto", [app, signed]);
    await step(t("sideload.resigning"), async (update) => {
      const entitlements = path.join(work, "entitlements.plist");
      fs.writeFileSync(entitlements, guestEntitlements(id));
      execFileSync("plutil", [
        "-replace",
        "AppGroupIdentifier",
        "-string",
        `${guestTeam}.${id}`,
        path.join(signed, "Info.plist"),
      ]);
      await resign(signed, id, { identity: "-", entitlements }, update);
    });
    await step(t("sideload.installing", { device: device.name }), () =>
      installApp(device, signed, id),
    );
  } finally {
    fs.rmSync(work, { recursive: true, force: true });
  }
}

async function unlockRunningIfPresent(
  context: Pick<Context, "signal">,
  deviceName: string,
) {
  const running = await step(
    t("sideload.checking", { device: deviceName }),
    () => runningSwipt(deviceName, context.signal),
    { transient: true },
  );
  if (running !== undefined) {
    await step(
      t("sideload.unlocking"),
      () => tryUnlock(running, context.signal),
      { transient: true },
    );
  }
}

async function configureAndOpenAndroid(
  context: SetupContext,
  device: AndroidDevice,
  passcode: Passcode | undefined,
) {
  const publishes = !context.options["no-setup-api"];
  const outcome = await step(
    t("sideload.opening", { device: device.name }),
    (update) => {
      if (passcode !== undefined) {
        update(t("sideload.settingUpAutoUnlock"));
      }
      return setupAndroidApp(device, {
        passcode,
        publishLocalNetwork: publishes,
        signal: context.signal,
        onWaitingPrivilege(mode) {
          update(
            mode === "Root"
              ? t("sideload.allowRoot", { device: device.name })
              : t("sideload.allowShizuku", { device: device.name }),
          );
        },
      });
    },
  );
  if (!outcome.ready) {
    console.log(t("sideload.needsPrivilege", { device: device.name }));
    return;
  }
  console.log(t("sideload.running", { app: "Swipt" }));
  if (!publishes) {
    return;
  }
  if (device.virtual && outcome.port !== undefined) {
    await forwardAndroidPort(device, outcome.port, context.signal);
    const base = `http://127.0.0.1:${outcome.port}`;
    const url = outcome.authenticates ? withToken(base) : base;
    console.log(
      columns([
        ["CLI", url],
        ["MCP", `${url}/mcp`],
      ]),
    );
    return;
  }
  await printAPI(context, device.name);
}

export async function installAndOpen(
  context: SetupContext,
  {
    device,
    id,
    current,
  }: {
    device: Device | TargetDevice;
    id: string;
    current: Application | undefined;
  },
  app: string,
  build: number,
  {
    passcode,
    saves = true,
  }: { passcode: Passcode | undefined; saves?: boolean },
) {
  if (current !== undefined) {
    await unlockRunningIfPresent(context, device.name);
  }
  if (device.platform === "android") {
    await step(t("sideload.installing", { device: device.name }), () =>
      installAndroidAPK(device, app, {
        downgrade:
          current !== undefined && Number(current.bundleVersion) >= build,
        signal: context.signal,
      }),
    );
    const result = await step(
      t("sideload.checking", { device: device.name }),
      () => androidSwiptApp(device, context.signal),
      { transient: true },
    );
    if (result === undefined || Number(result.bundleVersion) !== build) {
      throw new Error(
        result === undefined
          ? `${device.name} does not list ${id} after the install`
          : `${device.name} has ${buildText(result)} after the install`,
      );
    }
    const parameters = { build: buildText(result), device: device.name };
    console.log(
      current !== undefined && Number(current.bundleVersion) < build
        ? t("sideload.updated", parameters)
        : t("sideload.installed", parameters),
    );
    if (context.options["no-setup"]) {
      await step(t("sideload.opening", { device: device.name }), () =>
        launchAndroidApp(device, context.signal),
      );
      return;
    }
    await configureAndOpenAndroid(
      context,
      device,
      saves ? passcode : undefined,
    );
    return;
  }

  await prepareDiskImage(context, device);
  if (device.virtual) {
    await installOnGuest(device, id, app);
  } else {
    await step(t("sideload.installing", { device: device.name }), () =>
      installApp(device, app, id),
    );
  }
  if (saves && passcode !== undefined) {
    await step(t("sideload.settingUpAutoUnlock"), () =>
      setUpAutoUnlock(device, id, passcode),
    );
  }
  const result = (
    await step(
      t("sideload.checking", { device: device.name }),
      () => apps(device),
      { transient: true },
    )
  ).find((it) => it.bundleIdentifier === id);
  if (result === undefined || Number(result.bundleVersion) !== build) {
    throw new Error(
      result === undefined
        ? `${device.name} does not list ${id} after the install`
        : `${device.name} has ${buildText(result)} after the install`,
    );
  }
  const parameters = { build: buildText(result), device: device.name };
  console.log(
    current !== undefined && Number(current.bundleVersion) < build
      ? t("sideload.updated", parameters)
      : t("sideload.installed", parameters),
  );
  if (!device.virtual) {
    console.log(
      t("sideload.expiration", {
        expiration: expirationText(
          Date.parse(
            profileField(
              path.join(app, "embedded.mobileprovision"),
              "ExpirationDate",
            ),
          ),
        ),
      }),
    );
  }
  if (context.options["no-setup"]) {
    await launch(device, id);
    return;
  }
  const paired =
    !context.options["no-setup-pair"] && (await pair(context, device, id));
  await openConfigured(
    context,
    device,
    id,
    passcode,
    current !== undefined && !paired,
  );
}

async function printAPI(context: Pick<Context, "signal">, deviceName: string) {
  const found = await step(
    t("sideload.checking", { device: deviceName }),
    () => findPublished(deviceName, context.signal),
    { transient: true },
  );
  if (found === undefined) {
    console.error(t("sideload.notOnNetwork", { device: deviceName }));
    return;
  }
  const url = found.authenticates ? withToken(found.url) : found.url;
  console.log(
    columns([
      ["CLI", url],
      ["MCP", `${url}/mcp`],
    ]),
  );
}

async function openConfigured(
  context: SetupContext,
  device: Device,
  id: string,
  passcode: Passcode | undefined,
  updated = false,
) {
  const publishes = !context.options["no-setup-api"];
  if (device.virtual) {
    await step(t("sideload.opening", { device: device.name }), async () => {
      await savePreferences(device, id, {
        autoUnlockSetUp: true,
        developerLocalNetworkEnabled: publishes,
      });
      if (publishes) {
        await configureVirtual(device, id, "Swipt");
      }
      await launchApp(device, id);
    });
  } else {
    await trustAndOpen(context, device, id, passcode, updated);
  }
  if (publishes) {
    await printAPI(context, device.name);
  }
}

async function launch(device: Device, id: string) {
  try {
    await step(t("sideload.opening", { device: device.name }), () =>
      launchApp(device, id),
    );
    return true;
  } catch (error) {
    if (failedWith(error, failures.untrusted)) {
      await step(t("sideload.openingSettings", { device: device.name }), () =>
        openSettings(
          device,
          "prefs:root=General&path=ManagedConfigurationList",
        ),
      );
      console.log(t("sideload.untrusted", { device: device.name }));
    } else if (failedWith(error, failures.locked)) {
      console.log(t("sideload.opensAfterUnlock", { device: device.name }));
    } else {
      throw error;
    }
    return false;
  }
}

export async function install(context: DeployContext) {
  validatePasscodeType(context.options);
  const device = await targetDevice(context);
  if (device.platform === "android") {
    const current = await locateAndroid(context, device);
    await deployAndroid(context, device, current, false);
    return;
  }
  await deployIOS(
    context,
    { device, ...(await locateIOS(context, device, { fresh: true })) },
    false,
  );
}

export async function update(context: DeployContext) {
  validatePasscodeType(context.options);
  const { device, team, current } = await findInstalled(context);
  if (device.platform === "android") {
    await deployAndroid(context, device, current, true);
    return;
  }
  await deployIOS(context, { device, team, current }, true);
}

export async function setup(context: PasscodeSetupContext) {
  validatePasscodeType(context.options);
  const { device, current } = await findInstalled(context);
  const platform = device.platform;
  const given = passcodeOf(context.options, platform);
  const passcode =
    given ??
    (interactive(context) && !device.virtual
      ? await askPasscode(context, platform)
      : undefined);
  if (device.platform === "android") {
    await configureAndOpenAndroid(context, device, passcode);
    return;
  }
  const id = current.bundleIdentifier;
  await prepareDiskImage(context, device);
  if (passcode !== undefined) {
    await step(t("sideload.settingUpAutoUnlock"), () =>
      setUpAutoUnlock(device, id, passcode),
    );
  }
  if (!context.options["no-setup-pair"]) {
    await pair(context, device, id, true);
  }
  await openConfigured(context, device, id, passcode);
}

const trustMessages = JSON.stringify({
  unlockToContinue: t("sideload.unlockPrompt"),
  unlocking: t("sideload.unlocking"),
  trusting: t("sideload.trusting", {
    developer: "{developer}",
  }),
  running: t("sideload.running", { app: "{app}" }),
  trusted: t("sideload.trusted", { developer: "{developer}" }),
});

async function pair(
  context: Pick<Context, "signal">,
  device: Device,
  id: string,
  overwrite = false,
) {
  if (
    !overwrite &&
    (await step(
      t("sideload.checking", { device: device.name }),
      () => hasPairing(device, id),
      { transient: true },
    ))
  ) {
    return false;
  }
  try {
    await step(t("driver.pairing", { device: device.name }), () =>
      savePairing(device, id),
    );
  } catch (error) {
    if (context.signal.aborted) {
      throw error;
    }
    console.error(
      t("sideload.pairingFailed", {
        error: errorMessage(error),
      }),
    );
    return false;
  }
  return true;
}

async function launchUnlocking(
  device: Device,
  id: string,
  args: string[],
  signal: AbortSignal,
) {
  try {
    await launchApp(device, id, args);
    return;
  } catch (error) {
    if (!failedWith(error, failures.locked)) {
      throw error;
    }
    const passcode = await savedPasscode(device, id);
    if (passcode === undefined) {
      throw error;
    }
    const driven = serve(device.udid, {
      passcode,
      token: randomToken(),
      signal,
    });
    try {
      const failure = await tryUnlock(
        open((await driven.serving).local),
        signal,
      );
      if (failure !== undefined) {
        throw new Error(failure);
      }
    } finally {
      await driven.stop();
    }
  }
  await launchApp(device, id, args);
}

async function trustAndOpen(
  context: SetupContext,
  device: Device,
  id: string,
  given: Passcode | undefined,
  updated: boolean,
) {
  const publishes = !context.options["no-setup-api"];
  const lines: ConsoleOutput[] = [];
  let failure: ScriptError | undefined;
  let passcode = given;
  try {
    await step(
      t("sideload.opening", { device: device.name }),
      async (update) => {
        if (updated) {
          try {
            await launchUnlocking(
              device,
              id,
              publishes ? [publishArgument] : [],
              context.signal,
            );
            return;
          } catch (error) {
            if (
              !failedWith(error, failures.untrusted) &&
              !failedWith(error, failures.locked)
            ) {
              throw error;
            }
          }
        }
        passcode ??= await savedPasscode(device, id);
        await terminateApp(device, id);
        const unlock =
          passcode !== undefined
            ? "passcode"
            : process.stderr.isTTY
              ? "wait"
              : "";
        let driven;
        let local;
        for (;;) {
          driven = serve(device.udid, {
            passcode,
            token: randomToken(),
            signal: context.signal,
          });
          try {
            local = (await driven.serving).local;
            break;
          } catch (error) {
            if (
              !failedWith(error, failures.deviceNotListed) ||
              unlock !== "wait"
            ) {
              throw error;
            }
            update(t("sideload.waiting", { device: device.name }));
            await waitForDevice(device, context.signal);
          }
        }
        try {
          const client = open(local);
          const run = async (args: string[]) => {
            for await (const response of client.repl.execute(
              {
                code: fs.readFileSync(
                  new URL("../sideload/trust.js", import.meta.url),
                  "utf8",
                ),
                args: [trustMessages, ...args],
                program: true,
              },
              { signal: context.signal },
            )) {
              switch (response.part.case) {
                case "console": {
                  const output = response.part.value;
                  if (output.level === LogLevel.DEBUG) {
                    update(output.text);
                  } else {
                    lines.push(output);
                  }
                  break;
                }
                case "error":
                  failure = response.part.value;
                  break;
              }
            }
            if (failure) {
              throw new Error(failure.message);
            }
          };
          const args = publishes ? [setupArgument, publishArgument] : [];
          const promptArgs = publishes ? [] : ["no-prompts"];
          await run([id, "unlock", unlock, "", ...promptArgs]);
          update("");
          try {
            await launchApp(device, id, args);
          } catch (error) {
            if (!failedWith(error, failures.untrusted)) {
              throw error;
            }
            await run([id, "trust", "", error.message, ...promptArgs]);
            update("");
            await launchApp(device, id, args);
          }
          if (publishes) {
            await run([id, "prompts"]);
          }
        } finally {
          await driven.stop();
        }
      },
    );
  } catch (error) {
    if (failure === undefined) {
      throw error;
    }
  } finally {
    for (const output of lines) {
      (output.level === LogLevel.ERROR || output.level === LogLevel.WARN
        ? console.error
        : console.log)(output.text);
    }
  }
  if (failure) {
    if (
      passcode === undefined &&
      failure.name === "UnavailableError" &&
      /\bdevice is locked\b/.test(failure.message)
    ) {
      throw new Error(
        t("sideload.locked", { device: device.name, message: failure.message }),
      );
    }
    throw printScriptError(failure);
  }
}

export async function uninstall(context: PromptContext) {
  const installed: InstalledApps = {
    ios: new Map(),
    android: new Map(),
  };
  const device = await targetDevice(context, {
    checkSupport: false,
    filter: sideloadedFilter(installed, t("sideload.noneInstalled")),
  });
  if (device.platform === "android") {
    const current = await locateAndroid(context, device, installed.android);
    if (current === undefined) {
      throw new Error(t("sideload.notInstalled", { device: device.name }));
    }
    await step(t("sideload.removing", { device: device.name }), () =>
      uninstallAndroidApp(device, current.bundleIdentifier, context.signal),
    );
    console.log(
      t("sideload.removed", {
        build: buildText(current),
        device: device.name,
      }),
    );
    return;
  }
  const { team, current } = await locateIOS(context, device, {
    installed: installed.ios.get(device.udid),
  });
  if (current === undefined) {
    throw new Error(
      team === undefined
        ? t("sideload.notInstalled", { device: device.name })
        : t("sideload.notInstalledByTeam", {
            device: device.name,
            team,
          }),
    );
  }
  await step(t("sideload.removing", { device: device.name }), () =>
    uninstallApp(device, current.bundleIdentifier),
  );
  console.log(
    t("sideload.removed", {
      build: buildText(current),
      device: device.name,
    }),
  );
}
