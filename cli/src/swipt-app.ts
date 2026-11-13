import {
  apps,
  containerHasFile,
  containerSQL,
  pairApp,
  terminateApp,
} from "./driver";
import { passcodeTypes } from "./passcode";

export const bundleId = "io.nekohasekai.swipt";
export const bundleTeam = "P8XK3KHB48";
export const teamPattern = /^[A-Z0-9]{10}$/;
const sideloadedPattern = new RegExp(
  `^${bundleId.replaceAll(".", "\\.")}(?:\\.([A-Z0-9]{10}))?$`,
);

export const publishArgument = "--publish-local-network";
export const setupArgument = "--setup";

const databasePath = "Library/Application Support/Swipt.sqlite";
const pairingPath = "Library/Application Support/PairingRecord.plist";

const schema = [
  "CREATE TABLE grdb_migrations (identifier TEXT NOT NULL PRIMARY KEY)",
  'CREATE TABLE "preferences" ("name" TEXT PRIMARY KEY NOT NULL, "data" BLOB NOT NULL)',
  'CREATE TABLE "swiplets" ("id" TEXT PRIMARY KEY NOT NULL, "position" INTEGER NOT NULL, "etag" TEXT NOT NULL)',
  'CREATE TABLE "repositories" ("url" TEXT PRIMARY KEY NOT NULL, "position" INTEGER NOT NULL, "name" TEXT NOT NULL, "localizedNames" TEXT, "iconColor" TEXT, "iconSymbol" TEXT, "image" BLOB, "etag" TEXT NOT NULL)',
  'CREATE TABLE "repositorySwiplets" ("repository" TEXT NOT NULL REFERENCES "repositories"("url") ON DELETE CASCADE, "position" INTEGER NOT NULL, "id" TEXT NOT NULL, "name" TEXT NOT NULL, "localizedNames" TEXT NOT NULL, "description" TEXT NOT NULL, "version" TEXT NOT NULL, "versionCode" INTEGER NOT NULL, "iconColor" TEXT, "iconSymbol" TEXT, "url" TEXT NOT NULL, "sha256" TEXT NOT NULL, PRIMARY KEY ("repository", "id"))',
  "INSERT INTO grdb_migrations VALUES ('0.1.0')",
];

export const swiptApps = async (device) =>
  (await apps(device)).flatMap((app) => {
    const match = sideloadedPattern.exec(app.bundleIdentifier);
    return match === null ? [] : [{ ...app, team: match[1] ?? bundleTeam }];
  });

export async function savePreferences(device, id, preferences) {
  const exists = await containerHasFile(device, id, databasePath);
  if (exists) {
    await terminateApp(device, id);
  }
  const quote = (text) => `'${text.replaceAll("'", "''")}'`;
  await containerSQL(
    device,
    id,
    databasePath,
    [
      ...(exists ? [] : schema),
      ...Object.entries(preferences).map(
        ([name, value]) =>
          `INSERT OR REPLACE INTO preferences VALUES (${quote(name)}, CAST(${quote(JSON.stringify(value))} AS BLOB))`,
      ),
    ]
      .map((statement) => `${statement};\n`)
      .join(""),
  );
}

export const setUpAutoUnlock = (device, id, passcode) =>
  savePreferences(device, id, {
    autoUnlocks: true,
    autoUnlockPasscodeType: passcodeTypes.indexOf(passcode.type),
    autoUnlockPasscode: passcode.passcode,
    autoUnlockSetUp: true,
  });

export async function savedPasscode(device, id) {
  const rows = await containerSQL(
    device,
    id,
    databasePath,
    "SELECT name, CAST(data AS TEXT) AS value FROM preferences WHERE name LIKE 'autoUnlock%'",
    { query: true },
  );
  const saved = Object.fromEntries(
    rows.map(({ name, value }) => [name, JSON.parse(value)]),
  );
  return saved.autoUnlocks && saved.autoUnlockPasscode
    ? {
        passcode: saved.autoUnlockPasscode,
        type: passcodeTypes[saved.autoUnlockPasscodeType ?? 0],
      }
    : undefined;
}

export const hasPairing = (device, id) =>
  containerHasFile(device, id, pairingPath);

export const savePairing = (device, id) => pairApp(device, id, pairingPath);
