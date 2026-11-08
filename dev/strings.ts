import fs from "node:fs";
import path from "node:path";

import { parseSync, traverse } from "@babel/core";

function collectStrings(cli) {
  const keys = new Set<string>();
  for (const name of fs.globSync("src/*.ts", { cwd: cli })) {
    const file = path.join(cli, name);
    traverse(
      parseSync(fs.readFileSync(file, "utf8"), {
        babelrc: false,
        configFile: false,
        parserOpts: { plugins: ["typescript"] },
        sourceType: "module",
      }),
      {
        CallExpression({ node }) {
          if (node.callee.name !== "tr") {
            return;
          }
          const [key] = node.arguments;
          if (key?.type !== "StringLiteral") {
            throw new Error(
              `${file}:${node.loc.start.line}: tr takes a string literal`,
            );
          }
          keys.add(key.value);
        },
      },
    );
  }
  return keys;
}

const placeholders = (text, optional?) =>
  [
    ...new Set(
      [...text.matchAll(/\{(\w+)\}/g)]
        .map((match) => match[1])
        .filter((name) => name !== optional),
    ),
  ]
    .sort()
    .join();

function problemsOf(key, message) {
  if (typeof message === "string") {
    return placeholders(message) === placeholders(key)
      ? []
      : [`placeholders differ: ${key} -> ${message}`];
  }
  const forms = Object.values(message);
  const expected = placeholders(key, "count");
  return [
    ...("other" in message ? [] : [`no "other" form: ${key}`]),
    ...forms
      .filter((form) => placeholders(form, "count") !== expected)
      .map((form) => `placeholders differ: ${key} -> ${form}`),
  ];
}

export function syncLocales(cli) {
  const keys = [...collectStrings(cli)].sort();
  const directory = path.join(cli, "locales");
  const problems = [];
  for (const name of fs.readdirSync(directory).sort()) {
    const file = path.join(directory, name);
    const messages = JSON.parse(fs.readFileSync(file, "utf8"));
    const missing = keys.filter((key) => messages[key] === undefined);
    const present = keys.filter((key) => messages[key] !== undefined);
    problems.push(
      ...present.flatMap((key) =>
        problemsOf(key, messages[key]).map((it) => `${name}: ${it}`),
      ),
    );
    fs.writeFileSync(
      file,
      `${JSON.stringify(
        Object.fromEntries(present.map((key) => [key, messages[key]])),
        null,
        2,
      )}\n`,
    );
    console.log(
      `${path.basename(name, ".json")}: ${present.length} of ${keys.length}`,
    );
    for (const key of missing) {
      console.log(`  missing: ${key}`);
    }
  }
  if (problems.length > 0) {
    throw new Error(problems.join("\n"));
  }
}
