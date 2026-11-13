import { type StdioOptions, execFile, execFileSync } from "node:child_process";
import { X509Certificate, createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import { setTimeout as sleep } from "node:timers/promises";
import { promisify } from "node:util";

import select from "@inquirer/select";

import { UsageError, open } from "./client";
import {
  findPublished,
  runningSwipt,
  tryUnlock,
  withToken,
} from "./connection";
import { cacheDirectory } from "./directories";
import {
  apps,
  configureVirtual,
  failures,
  installApp,
  launchApp,
  mountDiskImage,
  openSettings,
  provisioningProfiles,
  randomToken,
  serve,
  terminateApp,
  trustedDevice,
  uninstallApp,
} from "./driver";
import { language, tr } from "./i18n";
import {
  choose,
  columns,
  interactive,
  printScriptError,
  selectTheme,
  step,
} from "./output";
import { askPasscode, passcodeOf } from "./passcode";
import { LogLevel } from "./proto/common_pb";
import {
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

const exec = promisify(execFile);

const repository = "swipt-automation/swipt";

const guestTeam = "SWIPTLOCAL";
// installd on a vphone guest accepts an ad hoc signature and needs
// application-identifier (vphone-cli 8c3630c7,
// VPhoneGuestComponents/MISFix/MISFixInstallPolicy.c).
const guestEntitlements = (id) => `<?xml version="1.0" encoding="UTF-8"?>
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

const signingNeedsMac = tr(
  'Installing or updating Swipt needs macOS; install it from a Mac, then set it up here with "swipt sideload setup".',
);

const expiresSoon = ({ creation, expiration }) =>
  expiration - Date.now() < (expiration - creation) / 2;

const plistValueMissing = /No value at that key path/;

function headers(accept) {
  const token = process.env.GITHUB_TOKEN;
  return {
    Accept: accept,
    "X-GitHub-Api-Version": "2022-11-28",
    "User-Agent": "swipt-cli",
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

async function github(url, accept, signal) {
  let response = await fetch(url, {
    headers: headers(accept),
    signal,
    redirect: "manual",
  });
  if (response.status >= 300 && response.status < 400) {
    response = await fetch(response.headers.get("location"), { signal });
  }
  if (!response.ok) {
    let message = `GitHub answered ${response.status} for ${url}`;
    if ([401, 403, 429].includes(response.status)) {
      message += `; ${process.env.GITHUB_TOKEN ? "GITHUB_TOKEN" : "set GITHUB_TOKEN to a token that"} must read the repository and stay under the rate limit`;
    }
    const error: Error & { status?: number } = new Error(message);
    error.status = response.status;
    throw error;
  }
  return response;
}

async function findRelease(nightly, signal) {
  const read = async (name): Promise<any> =>
    (
      await github(
        `https://api.github.com/repos/${repository}/releases/${name}`,
        "application/vnd.github+json",
        signal,
      )
    ).json();
  const release = nightly
    ? await read("tags/nightly")
    : await read("latest").catch((error) => {
        if (error.status !== 404) {
          throw error;
        }
        return read("tags/nightly");
      });
  const asset = (name) => {
    const found = release.assets.find((it) => it.name === name);
    if (!found) {
      throw new Error(`release ${release.tag_name} has no ${name}`);
    }
    return found;
  };
  const metadata: any = await (
    await github(asset("metadata.json").url, "application/octet-stream", signal)
  ).json();
  return {
    tag: release.tag_name,
    version: metadata.version,
    build: Number(metadata.build),
    ipa: asset("Swipt.ipa"),
  };
}

async function download(url, file, signal, update) {
  const response = await github(url, "application/octet-stream", signal);
  const total = Number(response.headers.get("content-length"));
  const megabytes = (bytes) => (bytes / 1_000_000).toFixed(1);
  let received = 0;
  await pipeline(
    Readable.fromWeb(response.body),
    async function* (source) {
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

async function unpack(ipa, directory) {
  await exec("ditto", ["-x", "-k", ipa, directory]);
  const payload = path.join(directory, "Payload");
  const app = fs.readdirSync(payload).find((name) => name.endsWith(".app"));
  if (app === undefined) {
    throw new Error("the IPA has no app in Payload");
  }
  return path.join(payload, app);
}

const pbxproj = (id, team) => `// !$*UTF8*$!
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

function explain(output, team) {
  const errors = [
    ...new Set(
      output
        .split("\n")
        .filter((line) => /^(xcodebuild: )?error: /.test(line))
        .map((line) => line.replace(/^(xcodebuild: )?error: /, "")),
    ),
  ].join("; ");
  if (/No Accounts?\b/.test(output)) {
    return tr(
      "Xcode has no account for team {team}; sign in to its Apple Account at {accounts}.",
      { team, accounts: accountsHint },
    );
  }
  if (/signing certificate|certificateType/i.test(output)) {
    return tr(
      "Xcode couldn't make a development certificate for team {team}; add one at {accounts} › Manage Certificates ({errors}).",
      { team, accounts: accountsHint, errors },
    );
  }
  if (/requires Xcode|xcode-select/.test(output)) {
    return tr(
      "xcodebuild needs Xcode; install it and select it with xcode-select",
    );
  }
  return errors || output.trim();
}

function profileField(file, key) {
  return execFileSync("plutil", ["-extract", key, "raw", "-o", "-", "-"], {
    input: execFileSync("security", ["cms", "-D", "-i", file], {
      stdio: ["ignore", "pipe", "ignore"],
    }),
    encoding: "utf8",
  }).trim();
}

function revoked(certificates) {
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

async function provision(work, id, team, device, update) {
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
    running.child.stdout.on("data", (chunk) => {
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
      if (error.code === "ENOENT") {
        throw new Error(tr("xcodebuild is missing; install Xcode."));
      }
      if (typeof error.code !== "number") {
        throw error;
      }
      throw new Error(explain(`${error.stdout}${error.stderr}`, team));
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

async function resign(app, id, signing, update) {
  const info = path.join(app, "Info.plist");
  execFileSync("plutil", [
    "-replace",
    "CFBundleIdentifier",
    "-string",
    id,
    info,
  ]);
  let permitted;
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
        { stdio: "pipe" },
      ) as unknown as string,
    );
  } catch (error) {
    if (!plistValueMissing.test(error.stderr)) {
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
  const teams = new Map();
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
  return [...teams].map(([id, name]) => ({ id, name }));
}

function xcodeSignedIn() {
  try {
    const lists = JSON.parse(
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
    return Object.values(lists).some(
      (accounts: unknown[]) => accounts.length > 0,
    );
  } catch (error) {
    if (plistValueMissing.test(error.stderr)) {
      return false;
    }
    throw error;
  }
}

const teamList = (teams) =>
  teams
    .map((team) => `  ${team.id}${team.name ? `  ${team.name}` : ""}`)
    .join("\n");

const teamChoice = ({ id, name }) => ({
  name: name ? `${name}  ${id}` : id,
  short: name ?? id,
  value: id,
});

async function sideloadedDevices(
  devices,
  missing = tr("Swipt isn't sideloaded on any iPhone or iPad."),
) {
  const sideloaded = await step(
    tr("looking for Swipt on the devices"),
    () =>
      Promise.all(
        devices.map((device) =>
          swiptApps(device).then(
            (found) => found.length > 0,
            () => true,
          ),
        ),
      ),
    { transient: true },
  );
  const found = devices.filter((_, index) => sideloaded[index]);
  if (found.length === 0) {
    throw new Error(missing);
  }
  return found;
}

async function locate(context, device, fresh = false) {
  const { options } = context;
  const found = await step(
    tr("checking Swipt on {device}", { device: device.name }),
    () => swiptApps(device),
    { transient: true },
  );
  if (device.virtual) {
    return {
      current: found.find((app) => app.bundleIdentifier === bundleId),
    };
  }
  if (options.team !== undefined) {
    if (!teamPattern.test(options.team)) {
      throw new UsageError(
        tr(
          "invalid argument {value} for --team: it's the 10-character team ID shown in {accounts}",
          { value: JSON.stringify(options.team), accounts: accountsHint },
        ),
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
      tr("Choose a team"),
      teams.map(teamChoice),
      new Error(
        `${tr("Swipt is sideloaded on {device} by more than one team; choose one with --team ID:", { device: device.name })}\n${teamList(teams)}`,
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
          ? tr(
              "Xcode has no development certificate; add one at {accounts} › Manage Certificates, or choose a team with --team ID",
              { accounts: accountsHint },
            )
          : tr(
              "Xcode has no account; sign in at {accounts}, or choose a team with --team ID",
              { accounts: accountsHint },
            ),
      );
    }
    const team = teams.some((it) => it.id === bundleTeam)
      ? bundleTeam
      : teams.length === 1
        ? teams[0].id
        : await choose(
            context,
            tr("Choose a team"),
            teams.map(teamChoice),
            new Error(
              `${tr("Choose a team with --team ID:")}\n${teamList(teams)}`,
            ),
          );
    return { team, current: undefined };
  }
  return { team: found[0]?.team, current: found[0] };
}

const buildText = (app) => `Swipt ${app.version} (${app.bundleVersion})`;

async function signingPeriod(device, id, team) {
  const names = [id, "*"].map((it) => `iOS Team Provisioning Profile: ${it}`);
  const profiles = await step(
    tr("checking Swipt on {device}", { device: device.name }),
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
    .reduce((latest, it) => (it.expiration > latest.expiration ? it : latest), {
      creation: 0,
      expiration: 0,
    });
}

function expirationText(time) {
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

export async function installPasscode(context, device, current) {
  return current === undefined &&
    !device.virtual &&
    interactive(context) &&
    context.options.passcode === undefined &&
    context.options["passcode-type"] === undefined &&
    !context.options["no-setup"]
    ? askPasscode(context)
    : passcodeOf(context.options);
}

async function findInstalled(context) {
  const device = await trustedDevice(context, {
    filter: (devices) =>
      sideloadedDevices(
        devices,
        tr(
          'Swipt isn\'t sideloaded on any iPhone or iPad; install it with "swipt sideload install".',
        ),
      ),
  });
  const { team, current } = await locate(context, device);
  if (current === undefined) {
    throw new Error(
      team === undefined
        ? tr(
            'Swipt isn\'t sideloaded on {device}; install it with "swipt sideload install".',
            { device: device.name },
          )
        : tr(
            'Swipt isn\'t sideloaded on {device} by team {team}; install it with "swipt sideload install".',
            { device: device.name, team },
          ),
    );
  }
  return { device, team, current };
}

async function downloadIPA(context, release) {
  const name = `Swipt-${release.ipa.id}.ipa`;
  const ipa = path.join(downloads, name);
  if (fs.existsSync(ipa)) {
    return ipa;
  }
  fs.mkdirSync(downloads, { recursive: true });
  const partial = `${ipa}.download`;
  await step(
    tr("downloading Swipt {version} ({build}) from {release}", {
      version: release.version,
      build: release.build,
      release: release.tag,
    }),
    (update) => download(release.ipa.url, partial, context.signal, update),
  );
  fs.renameSync(partial, ipa);
  for (const entry of fs.readdirSync(downloads)) {
    if (entry !== name) {
      fs.rmSync(path.join(downloads, entry), { force: true });
    }
  }
  return ipa;
}

async function prepareDiskImage(context, device) {
  if (device.virtual) {
    return;
  }
  try {
    await step(
      tr("preparing the Developer Disk Image on {device}", {
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
      tr("couldn't prepare the Developer Disk Image on {device}: {error}", {
        device: device.name,
        error: error.message,
      }),
    );
  }
}

async function deploy(
  context,
  { device, team, current }: { device: any; team?: any; current: any },
  updateOnly,
) {
  const passcode = await installPasscode(context, device, current);
  const release = await step(
    tr("checking the latest release"),
    () => findRelease(context.options.nightly, context.signal),
    { transient: true },
  );
  const id =
    device.virtual || team === bundleTeam ? bundleId : `${bundleId}.${team}`;
  if (current !== undefined) {
    const installed = Number(current.bundleVersion);
    const signing = device.virtual
      ? undefined
      : await signingPeriod(device, id, team);
    const parameters = {
      build: buildText(current),
      device: device.name,
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
          ? tr("{build} on {device} is already {release}.", parameters)
          : tr(
              "{build} on {device} is later than {release} ({releaseBuild}).",
              parameters,
            ),
      );
    }
    const versions = {
      device: device.name,
      version: `${current.version} (${current.bundleVersion})`,
      releaseVersion: `${release.version} (${release.build})`,
    };
    const replaces = asks
      ? await select(
          {
            message:
              release.tag === "nightly"
                ? tr(
                    "Downgrade Swipt on {device} from {version} to {releaseVersion}? Swipt resets its data when the earlier version can't read it.",
                    versions,
                  )
                : tr(
                    "Downgrade Swipt on {device} from nightly {version} to stable {releaseVersion}? Swipt resets its data when the earlier version can't read it.",
                    versions,
                  ),
            theme: selectTheme,
            choices: [
              { name: tr("Downgrade"), value: true },
              { name: tr("No Thanks"), value: false },
            ],
          },
          { output: process.stderr, signal: context.signal },
        )
      : !updateOnly && context.options.force;
    if (
      !replaces &&
      installed >= release.build &&
      (signing === undefined || !expiresSoon(signing))
    ) {
      if (signing !== undefined) {
        console.log(
          tr("Its signing expires {expiration}.", {
            expiration: expirationText(signing.expiration),
          }),
        );
      }
      const saves = !updateOnly && passcode !== undefined;
      const pairs =
        !context.options["no-setup"] &&
        !context.options["no-setup-pair"] &&
        !(await step(
          tr("checking Swipt on {device}", { device: device.name }),
          () => hasPairing(device, id),
          { transient: true },
        ));
      if (!saves && !pairs) {
        return;
      }
      await prepareDiskImage(context, device);
      if (saves) {
        await step(tr("setting up auto-unlock"), () =>
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
      throw new Error(
        tr(
          '{build} on {device} needs new signing, but {release} ({releaseBuild}) is earlier; use --nightly, or downgrade with "swipt sideload install --force".',
          parameters,
        ),
      );
    }
  }
  if (!device.virtual && process.platform !== "darwin") {
    throw new Error(signingNeedsMac);
  }
  const work = fs.mkdtempSync(path.join(os.tmpdir(), "swipt-sideload-"));
  try {
    const signing = device.virtual
      ? undefined
      : await step(
          tr("preparing signing for Swipt with team {team} through Xcode", {
            team: xcodeTeams().find((it) => it.id === team)?.name ?? team,
          }),
          (update) => provision(work, id, team, device, update),
        );
    const ipa = await downloadIPA(context, release);
    const app =
      signing === undefined
        ? await unpack(ipa, work)
        : await step(tr("re-signing Swipt"), async (update) => {
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

async function installOnGuest(device, id, app) {
  const work = fs.mkdtempSync(path.join(os.tmpdir(), "swipt-guest-"));
  try {
    const signed = path.join(work, "Payload", path.basename(app));
    await exec("ditto", [app, signed]);
    await step(tr("re-signing Swipt"), async (update) => {
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
    await step(tr("installing on {device}", { device: device.name }), () =>
      installApp(device, signed, id),
    );
  } finally {
    fs.rmSync(work, { recursive: true, force: true });
  }
}

export async function installAndOpen(
  context,
  { device, id, current },
  app,
  build,
  { passcode, saves = true },
) {
  if (current !== undefined) {
    const running = await step(
      tr("checking Swipt on {device}", { device: device.name }),
      () => runningSwipt(device.name, context.signal),
      { transient: true },
    );
    if (running !== undefined) {
      await step(
        tr("unlocking the device"),
        () => tryUnlock(running, context.signal),
        { transient: true },
      );
    }
  }
  await prepareDiskImage(context, device);
  if (device.virtual) {
    await installOnGuest(device, id, app);
  } else {
    await step(tr("installing on {device}", { device: device.name }), () =>
      installApp(device, app, id),
    );
  }
  if (saves && passcode !== undefined) {
    await step(tr("setting up auto-unlock"), () =>
      setUpAutoUnlock(device, id, passcode),
    );
  }
  const result = (
    await step(
      tr("checking Swipt on {device}", { device: device.name }),
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
      ? tr("{build} is updated on {device}.", parameters)
      : tr("{build} is installed on {device}.", parameters),
  );
  if (!device.virtual) {
    console.log(
      tr("Its signing expires {expiration}.", {
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

async function printAPI(context, device) {
  const found = await step(
    tr("checking Swipt on {device}", { device: device.name }),
    () => findPublished(device.name, context.signal),
    { transient: true },
  );
  if (found === undefined) {
    console.error(
      tr(
        "{device} wasn't found on the local network; find its API URLs in Swipt › Developer › Local Network.",
        { device: device.name },
      ),
    );
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

async function openConfigured(context, device, id, passcode, updated?) {
  const publishes = !context.options["no-setup-api"];
  if (device.virtual) {
    await step(
      tr("opening Swipt on {device}", { device: device.name }),
      async () => {
        await savePreferences(device, id, {
          autoUnlockSetUp: true,
          developerLocalNetworkEnabled: publishes,
        });
        await configureVirtual(device, id, "Swipt");
        await launchApp(device, id);
      },
    );
  } else {
    await trustAndOpen(context, device, id, passcode, updated);
  }
  if (publishes) {
    await printAPI(context, device);
  }
}

async function launch(device, id, args?) {
  try {
    await step(tr("opening Swipt on {device}", { device: device.name }), () =>
      launchApp(device, id, args),
    );
    return true;
  } catch (error) {
    if (error.code === failures.untrusted) {
      await step(
        tr("opening Settings on {device}", { device: device.name }),
        () =>
          openSettings(
            device,
            "prefs:root=General&path=ManagedConfigurationList",
          ),
      );
      console.log(
        tr(
          "Its developer isn't trusted yet; trust it in the Settings page opened on {device}, then open Swipt.",
          { device: device.name },
        ),
      );
    } else if (error.code === failures.locked) {
      console.log(
        tr("It opens after {device} is unlocked.", { device: device.name }),
      );
    } else {
      throw error;
    }
    return false;
  }
}

export async function install(context) {
  const device = await trustedDevice(context);
  await deploy(
    context,
    { device, ...(await locate(context, device, true)) },
    false,
  );
}

export async function update(context) {
  await deploy(context, await findInstalled(context), true);
}

export async function setup(context) {
  const given = passcodeOf(context.options);
  const { device, current } = await findInstalled(context);
  const id = current.bundleIdentifier;
  const passcode =
    given ??
    (interactive(context) && !device.virtual
      ? await askPasscode(context)
      : undefined);
  await prepareDiskImage(context, device);
  if (passcode !== undefined) {
    await step(tr("setting up auto-unlock"), () =>
      setUpAutoUnlock(device, id, passcode),
    );
  }
  if (!context.options["no-setup-pair"]) {
    await pair(context, device, id, true);
  }
  await openConfigured(context, device, id, passcode);
}

const trustMessages = JSON.stringify({
  unlockToContinue: tr("Unlock the device to continue."),
  unlocking: tr("unlocking the device"),
  trusting: tr("trusting {developer} in Settings", {
    developer: "{developer}",
  }),
  running: tr("{app} is running.", { app: "{app}" }),
  trusted: tr("{developer} is trusted.", { developer: "{developer}" }),
});

async function pair(context, device, id, overwrite = false) {
  if (
    !overwrite &&
    (await step(
      tr("checking Swipt on {device}", { device: device.name }),
      () => hasPairing(device, id),
      { transient: true },
    ))
  ) {
    return false;
  }
  try {
    await step(tr("pairing with {device}", { device: device.name }), () =>
      savePairing(device, id),
    );
  } catch (error) {
    if (context.signal.aborted) {
      throw error;
    }
    console.error(
      tr("Pairing failed, so Swipt pairs in Settings instead: {error}", {
        error: error.message,
      }),
    );
    return false;
  }
  return true;
}

async function launchUnlocking(device, id, args, signal) {
  try {
    await launchApp(device, id, args);
    return;
  } catch (error) {
    if (error.code !== failures.locked) {
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

async function trustAndOpen(context, device, id, given, updated) {
  const lines = [];
  let failure;
  let passcode = given;
  try {
    await step(
      tr("opening Swipt on {device}", { device: device.name }),
      async (update) => {
        if (updated) {
          try {
            await launchUnlocking(
              device,
              id,
              context.options["no-setup-api"] ? [] : [publishArgument],
              context.signal,
            );
            return;
          } catch (error) {
            if (
              error.code !== failures.untrusted &&
              error.code !== failures.locked
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
        while (local === undefined) {
          driven = serve(device.udid, {
            passcode,
            token: randomToken(),
            signal: context.signal,
          });
          try {
            local = (await driven.serving).local;
          } catch (error) {
            if (error.code !== failures.deviceNotListed || unlock !== "wait") {
              throw error;
            }
            update(tr("waiting for {device}", { device: device.name }));
            await sleep(1_000, undefined, { signal: context.signal });
          }
        }
        try {
          const client = open(local);
          const run = async (args) => {
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
          const args = [
            setupArgument,
            ...(context.options["no-setup-api"] ? [] : [publishArgument]),
          ];
          await run([id, "unlock", unlock]);
          update("");
          try {
            await launchApp(device, id, args);
          } catch (error) {
            if (error.code !== failures.untrusted) {
              throw error;
            }
            await run([id, "trust", "", error.message]);
            update("");
            await launchApp(device, id, args);
          }
          await run([id, "prompts"]);
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
        tr(
          "{device}: {message}; unlock it or pass --passcode, then run this again.",
          { device: device.name, message: failure.message },
        ),
      );
    }
    throw printScriptError(failure);
  }
}

export async function uninstall(context) {
  const device = await trustedDevice(context, { filter: sideloadedDevices });
  const { team, current } = await locate(context, device);
  if (current === undefined) {
    throw new Error(
      team === undefined
        ? tr("Swipt isn't sideloaded on {device}.", { device: device.name })
        : tr("Swipt isn't sideloaded on {device} by team {team}.", {
            device: device.name,
            team,
          }),
    );
  }
  await step(tr("removing Swipt from {device}", { device: device.name }), () =>
    uninstallApp(device, current.bundleIdentifier),
  );
  console.log(
    tr("removed {build} from {device}", {
      build: buildText(current),
      device: device.name,
    }),
  );
}
