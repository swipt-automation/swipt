import { execFileSync } from "node:child_process";

const [base] = process.argv.slice(2);
const declarations = [
  "api",
  "internal/automation/runtime/src/extension/types",
  "internal/automation/runtime/src/extension/web.d.ts",
];

const git = (...args) =>
  execFileSync("git", args, { encoding: "utf8", maxBuffer: 1 << 26 });

function normalize(source) {
  const literals = [];
  let out = "";
  let index = 0;
  while (index < source.length) {
    if (source.startsWith("//", index)) {
      const end = source.indexOf("\n", index);
      index = end < 0 ? source.length : end;
    } else if (source.startsWith("/*", index)) {
      const end = source.indexOf("*/", index + 2);
      index = end < 0 ? source.length : end + 2;
      out += " ";
    } else if (`"'\``.includes(source[index])) {
      let end = index + 1;
      while (end < source.length && source[end] !== source[index]) {
        end += source[end] === "\\" ? 2 : 1;
      }
      out += `\uE000${literals.push(source.slice(index, end + 1)) - 1}\uE001`;
      index = end + 1;
    } else {
      out += source[index];
      index += 1;
    }
  }
  return out
    .trim()
    .replace(/\s+/g, " ")
    .replace(/ ?([^\w$ ]) ?/g, "$1")
    .replace(/,([)\]}>])/g, "$1")
    .replace(/\uE000(\d+)\uE001/g, (_, literal) => literals[literal]);
}

function surface(ref) {
  const manifest = JSON.parse(git("show", `${ref}:api/package.json`));
  const files = git("ls-tree", "-r", "--name-only", ref, "--", ...declarations)
    .split("\n")
    .filter((file) => file.endsWith(".d.ts") && !file.endsWith("/repl.d.ts"));
  return {
    level: manifest.apiLevel,
    parts: new Map([
      ["api/package.json exports", JSON.stringify(manifest.exports)],
      [
        "api/package.json peerDependencies",
        JSON.stringify(manifest.peerDependencies),
      ],
      ...files.map((file) => [file, normalize(git("show", `${ref}:${file}`))]),
    ]),
  };
}

function fail(message) {
  console.error(message);
  process.exit(1);
}

const previous = surface(base);
const current = surface("HEAD");
if (!Number.isInteger(current.level) || current.level < 1) {
  fail("apiLevel in api/package.json must be a positive integer");
}
if (previous.level !== undefined) {
  if (current.level < previous.level) {
    fail(
      `apiLevel in api/package.json went down from ${previous.level} to ${current.level}`,
    );
  }
  const changed = [
    ...new Set([...previous.parts.keys(), ...current.parts.keys()]),
  ].filter((name) => previous.parts.get(name) !== current.parts.get(name));
  if (changed.length > 0 && current.level === previous.level) {
    fail(
      `the API changed since ${base}, but apiLevel in api/package.json is still ${current.level}:\n${changed.map((name) => `  ${name}`).join("\n")}`,
    );
  }
}
