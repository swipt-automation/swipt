import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";

import { checkout } from "../../../checkout";
import { UsageError } from "../../../errors";
import { t } from "../../../i18n";
import { isOS, type OS } from "../../../platform";
import { type Context, defineCommand } from "../../command";

const { version }: { version: string } = JSON.parse(
  fs.readFileSync(new URL("../../../../package.json", import.meta.url), "utf8"),
);
const require = createRequire(import.meta.url);
const {
  peerDependencies,
  minimumIOSVersion,
  minimumAndroidSDK,
}: {
  peerDependencies: Partial<Record<string, string>>;
  minimumIOSVersion: number;
  minimumAndroidSDK: number;
} = require("@swipt/api/package.json");

const swiptPackage = (name: string) =>
  checkout !== undefined
    ? `file:${path.join(checkout, name)}`
    : version.includes("-")
      ? version
      : `>=${version}-0 <=${version}`;

const idOf = (name: string) =>
  name
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^[._-]+/, "")
    .slice(0, 128)
    .replace(/[._-]+$/, "") || "swiplet";

const defaultSymbols: { readonly [Name in OS]: string } = {
  ios: "sparkles",
  android: "auto_awesome",
};

const declarations: { readonly [Name in OS]: object } = {
  ios: { minimumVersion: minimumIOSVersion },
  android: { minimumSDK: minimumAndroidSDK },
};

const foregroundSources: { readonly [Name in OS]: string } = {
  ios: `import * as device from "@swipt/api/ios/device";

export const foregroundApp = (signal: AbortSignal) =>
  device.foregroundApplication.get({ signal });
`,
  android: `import * as applications from "@swipt/api/android/applications";

export const foregroundApp = (signal: AbortSignal) =>
  applications.foreground.get({ signal });
`,
};

const templates = (name: string, platforms: readonly OS[]) => ({
  "package.json": `${JSON.stringify(
    {
      name: idOf(name),
      version: "0.1.0",
      private: true,
      swiplet: {
        id: idOf(name),
        name,
        versionCode: 1,
        icon: {
          color: "blue",
          ...Object.fromEntries(
            platforms.map((os) => [os, defaultSymbols[os]]),
          ),
        },
        platforms: Object.fromEntries(
          platforms.map((os) => [os, declarations[os]]),
        ),
        app: "./src/App",
        extension: "./src/extension",
      },
      scripts: { build: "swipt project build", check: "tsc" },
      dependencies: { zod: "^4.6.7" },
      devDependencies: {
        "@swipt/api": swiptPackage("api"),
        "@swipt/cli": swiptPackage("cli"),
        typescript: "^7.0.2",
        ...Object.fromEntries(
          ["@expo/ui", "@types/react", "react"].map((name) => [
            name,
            peerDependencies[name],
          ]),
        ),
      },
    },
    null,
    2,
  )}\n`,
  "tsconfig.json": `${JSON.stringify(
    {
      compilerOptions: {
        target: "ES2022",
        module: "ESNext",
        moduleResolution: "Bundler",
        jsx: "react-jsx",
        lib: ["ES2023", "DOM"],
        strict: true,
        noEmit: true,
        skipLibCheck: true,
      },
    },
    null,
    2,
  )}\n`,
  ".gitignore": "node_modules/\ndist/\n",
  "src/foreground.d.ts":
    "export declare function foregroundApp(signal: AbortSignal): Promise<string>;\n",
  ...Object.fromEntries(
    platforms.map((os) => [`src/foreground.${os}.ts`, foregroundSources[os]]),
  ),
  "src/extension.ts": `import { defineAction, defineExtension } from "@swipt/api/swiplet";

import { foregroundApp } from "./foreground";

export default defineExtension({
  actions: {
    frontApp: defineAction({
      title: "Find the App in Front",
      run: (_, { signal }) => foregroundApp(signal),
    }),
  },
});
`,
  "src/App.tsx": `import { Button, Column, Host, Text } from "@expo/ui";
import { useState } from "react";
import { actions } from "@swipt/api/swiplet";

import type extension from "./extension";

const { frontApp } = actions<typeof extension>();

export default function App() {
  const [app, setApp] = useState("");
  return (
    <Host style={{ flex: 1 }}>
      <Column spacing={12}>
        <Button label="Find the App in Front" onPress={async () => setApp(await frontApp())} />
        {app === "" ? null : <Text>{app}</Text>}
      </Column>
    </Host>
  );
}
`,
});

const quote = (value: string) =>
  process.platform === "win32"
    ? /^[\w.:\\/-]+$/.test(value)
      ? value
      : `"${value}"`
    : /^[\w@%+=:,./-]+$/.test(value)
      ? value
      : `'${value.replaceAll("'", "'\\''")}'`;

function createCommand(directory: string, list: string | undefined) {
  const platforms = [...new Set(list?.split(",").map((os) => os.trim()))];
  if (platforms.length === 0 || !platforms.every(isOS)) {
    throw new UsageError(t("commands.project.create.invalidPlatforms"));
  }
  const root = path.resolve(directory);
  if (fs.existsSync(root) && fs.readdirSync(root).length > 0) {
    throw new Error(t("commands.project.create.notEmpty", { directory }));
  }
  for (const [file, contents] of Object.entries(
    templates(path.basename(root), platforms),
  )) {
    fs.mkdirSync(path.dirname(path.join(root, file)), { recursive: true });
    fs.writeFileSync(path.join(root, file), contents);
  }
  const manager = process.env.npm_config_user_agent?.split("/")[0] ?? "npm";
  console.log(
    `cd ${quote(directory)}\n${manager} install\n${manager} run build`,
  );
}

export const create = defineCommand({
  name: "create",
  use: "create DIRECTORY --platforms OS[,OS]",
  short: t("commands.project.create.description"),
  args: [1, 1],
  options: {
    platforms: {
      type: "string",
      value: "OS[,OS]",
      help: t("commands.project.create.platforms"),
    },
  },
  run: ({ args, options }: Context<{ platforms?: string }>) =>
    createCommand(args[0], options.platforms),
});
