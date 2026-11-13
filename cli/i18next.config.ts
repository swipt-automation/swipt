import fs from "node:fs";

import { defineConfig } from "i18next-cli";

export default defineConfig({
  locales: fs
    .readdirSync("locales")
    .sort((a, b) => Number(b === "en") - Number(a === "en")),
  extract: {
    input: ["src/**/*.ts"],
    output: "locales/{{language}}/{{namespace}}.json",
    primaryLanguage: "en",
  },
  types: {
    input: ["locales/en/*.json"],
    output: "src/i18next.d.ts",
  },
});
