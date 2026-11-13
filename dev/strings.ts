import fs from "node:fs";
import path from "node:path";

import { parseSync, traverse } from "@babel/core";

function collectStrings(cli: string) {
  const keys = new Set<string>();
  for (const name of fs.globSync("src/**/*.ts", { cwd: cli })) {
    const file = path.join(cli, name);
    const parsed = parseSync(fs.readFileSync(file, "utf8"), {
      babelrc: false,
      configFile: false,
      parserOpts: { plugins: ["typescript"] },
      sourceType: "module",
    });
    if (parsed === null) {
      continue;
    }
    traverse(parsed, {
      CallExpression({ node }) {
        if (node.callee.type !== "Identifier" || node.callee.name !== "tr") {
          return;
        }
        const where = `${file}:${node.loc?.start.line}`;
        const [source, , context] = node.arguments;
        const key =
          source?.type === "ObjectExpression"
            ? source.properties.flatMap((it) =>
                it.type === "ObjectProperty" &&
                it.key.type === "Identifier" &&
                it.key.name === "other"
                  ? [it.value]
                  : [],
              )[0]
            : source;
        if (key?.type !== "StringLiteral") {
          throw new Error(
            `${where}: tr takes a string literal or its plural forms`,
          );
        }
        if (context !== undefined && context.type !== "StringLiteral") {
          throw new Error(`${where}: tr takes its context as a string literal`);
        }
        if (/^\[[^\]]*\] /.test(key.value)) {
          throw new Error(
            `${where}: a string that starts like a context: ${key.value}`,
          );
        }
        keys.add(
          context === undefined ? key.value : `[${context.value}] ${key.value}`,
        );
      },
    });
  }
  return keys;
}

const placeholders = (text: string, optional?: string) =>
  [
    ...new Set(
      [...text.matchAll(/\{(\w+)\}/g)]
        .map((match) => match[1])
        .filter((name) => name !== optional),
    ),
  ]
    .sort()
    .join();

function problemsOf(key: string, message: string | Record<string, string>) {
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

export function syncLocales(cli: string) {
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
