import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";

import { tr } from "../../../i18n";
import { defineCommand } from "../../command";

const { version } = JSON.parse(
  fs.readFileSync(new URL("../../../../package.json", import.meta.url), "utf8"),
);
const require = createRequire(import.meta.url);
const { peerDependencies } = require("@swipt/api/package.json");

const swiptRange = version.includes("-")
  ? version
  : `>=${version}-0 <=${version}`;

const idOf = (name) =>
  name
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/^[._-]+/, "")
    .slice(0, 128)
    .replace(/[._-]+$/, "") || "swiplet";

const templates = {
  "package.json": (name) =>
    `${JSON.stringify(
      {
        name: idOf(name),
        version: "0.1.0",
        private: true,
        swiplet: {
          id: idOf(name),
          name,
          versionCode: 1,
          icon: { color: "blue", symbol: "sparkles" },
          app: "./src/App.tsx",
          extension: "./src/extension.ts",
        },
        scripts: { build: "swipt project build", check: "tsc" },
        dependencies: { zod: "^4.6.7" },
        devDependencies: {
          "@swipt/api": swiptRange,
          "@swipt/cli": swiptRange,
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
  "tsconfig.json": () =>
    `${JSON.stringify(
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
  ".gitignore": () => "node_modules/\ndist/\n",
  "src/extension.ts": () => `import * as device from "@swipt/api/device";
import { defineAction, defineExtension } from "@swipt/api/swiplet";

export default defineExtension({
  actions: {
    frontApp: defineAction({
      title: "Find the App in Front",
      run: (_, { signal }) => device.foregroundApplication.get({ signal }),
    }),
  },
});
`,
  "src/App.tsx":
    () => `import { Button, Form, Host, Section, Text } from "@expo/ui/swift-ui";
import { useState } from "react";
import { actions } from "@swipt/api/swiplet";

import type extension from "./extension";

const { frontApp } = actions<typeof extension>();

export default function App() {
  const [app, setApp] = useState("");
  return (
    <Host style={{ flex: 1 }}>
      <Form>
        <Section>
          <Button label="Find the App in Front" onPress={async () => setApp(await frontApp())} />
          {app === "" ? null : <Text>{app}</Text>}
        </Section>
      </Form>
    </Host>
  );
}
`,
};

const quote = (value) =>
  /^[\w@%+=:,./-]+$/.test(value)
    ? value
    : `'${value.replaceAll("'", "'\\''")}'`;

function createCommand(directory) {
  const root = path.resolve(directory);
  if (fs.existsSync(root) && fs.readdirSync(root).length > 0) {
    throw new Error(tr("{directory} is not empty", { directory }));
  }
  for (const [file, template] of Object.entries(templates)) {
    fs.mkdirSync(path.dirname(path.join(root, file)), { recursive: true });
    fs.writeFileSync(path.join(root, file), template(path.basename(root)));
  }
  const manager = process.env.npm_config_user_agent?.split("/")[0] ?? "npm";
  console.log(
    `cd ${quote(directory)} && ${manager} install && ${manager} run build`,
  );
}

export const create = defineCommand({
  name: "create",
  use: "create DIRECTORY",
  short: tr("Make a swiplet project in a new or empty directory"),
  args: [1, 1],
  run: ({ args }) => createCommand(args[0]),
});
