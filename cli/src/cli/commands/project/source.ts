import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";

import { strFromU8, unzipSync } from "fflate";

import { tr } from "../../../i18n";
import { errorMessage } from "../../../output";
import { idPattern } from "../../../project";
import { defineCommand } from "../../command";

interface Icon {
  symbol: string;
  color?: string;
  symbolColor?: string;
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null;

const isStringRecord = (value: unknown): value is Record<string, string> =>
  isRecord(value) &&
  Object.values(value).every((name) => typeof name === "string");

function stringField(
  record: Record<string, unknown>,
  name: string,
  field: string,
) {
  const value = record[name];
  if (value !== undefined && typeof value !== "string") {
    throw new Error(
      tr('"{field}" in package.json must be a string.', { field }),
    );
  }
  return value;
}

function loadIcon(icon: unknown): Icon | undefined {
  if (icon === undefined) {
    return undefined;
  }
  if (
    !isRecord(icon) ||
    typeof icon.symbol !== "string" ||
    (icon.color !== undefined && typeof icon.color !== "string")
  ) {
    throw new Error(
      tr(
        '"source.icon" in package.json must have a symbol and may have a color.',
      ),
    );
  }
  return {
    symbol: icon.symbol,
    color: icon.color,
    symbolColor: stringField(icon, "symbolColor", "source.icon.symbolColor"),
  };
}

function workspacePatterns(workspaces: unknown) {
  const [field, patterns] = Array.isArray(workspaces)
    ? ["workspaces", workspaces]
    : isRecord(workspaces) && Array.isArray(workspaces.packages)
      ? ["workspaces.packages", workspaces.packages]
      : ["", []];
  return patterns.map((pattern: unknown, index) => {
    if (typeof pattern !== "string") {
      throw new Error(
        tr('"{field}" in package.json must be a string.', {
          field: `${field}[${index}]`,
        }),
      );
    }
    return pattern;
  });
}

function loadSource(root: string) {
  const parsed: unknown = JSON.parse(
    fs.readFileSync(path.join(root, "package.json"), "utf8"),
  );
  const manifest = isRecord(parsed) ? parsed : {};
  const config = isRecord(manifest.source) ? manifest.source : {};
  const name = stringField(config, "name", "source.name");
  const image = stringField(config, "image", "source.image");
  const url = stringField(config, "url", "source.url");
  const localizedNames = config.localizedNames ?? {};
  if (!isStringRecord(localizedNames)) {
    throw new Error(
      tr(
        '"source.localizedNames" in package.json must map languages to names.',
      ),
    );
  }
  const icon = loadIcon(config.icon);
  let prefix: URL | undefined;
  if (url !== undefined) {
    try {
      prefix = new URL(url.endsWith("/") ? url : `${url}/`);
    } catch {
      throw new Error(
        tr('"source.url" in package.json isn\'t an absolute URL.'),
      );
    }
  }
  const workspaces = workspacePatterns(manifest.workspaces);
  return { root, name, image, localizedNames, icon, prefix, workspaces };
}

function workspaceSwiplets(source: ReturnType<typeof loadSource>) {
  const files: string[] = [];
  for (const pattern of source.workspaces) {
    for (const directory of fs.globSync(pattern, { cwd: source.root }).sort()) {
      const project = path.join(source.root, directory, "package.json");
      if (!fs.existsSync(project)) {
        continue;
      }
      const manifest: unknown = JSON.parse(fs.readFileSync(project, "utf8"));
      const id =
        isRecord(manifest) && isRecord(manifest.swiplet)
          ? manifest.swiplet.id
          : undefined;
      if (typeof id !== "string") {
        continue;
      }
      const built = path.join(source.root, directory, "dist", `${id}.swiplet`);
      if (!fs.existsSync(built)) {
        throw new Error(
          tr("{file} is missing; build {directory} first.", {
            file: path.relative(source.root, built),
            directory,
          }),
        );
      }
      files.push(built);
    }
  }
  return files;
}

function readSwiplet(file: string) {
  const data = fs.readFileSync(file);
  let entry;
  try {
    entry = unzipSync(data, {
      filter: ({ name }) => name === "manifest.json",
    })["manifest.json"];
  } catch (error) {
    throw new Error(`${file}: ${errorMessage(error)}`);
  }
  if (entry === undefined) {
    throw new Error(`${file} has no manifest.json`);
  }
  const manifest: unknown = JSON.parse(strFromU8(entry));
  if (
    !isRecord(manifest) ||
    typeof manifest.id !== "string" ||
    !idPattern.test(manifest.id)
  ) {
    throw new Error(`${file} has no valid swiplet id in its manifest`);
  }
  return { file, id: manifest.id, manifest, data };
}

function sourceCommand(root: string, given: string[]) {
  const source = loadSource(root);
  const files =
    given.length > 0
      ? given.map((file) => path.resolve(file))
      : workspaceSwiplets(source);
  if (files.length === 0) {
    throw new Error(
      tr(
        "package.json lists no workspaces with swiplets; name the swiplets to list.",
      ),
    );
  }
  const swiplets = new Map<string, ReturnType<typeof readSwiplet>>();
  for (const read of files.map(readSwiplet)) {
    const first = swiplets.get(read.id);
    if (first !== undefined) {
      throw new Error(
        tr("{id} is both {first} and {second}", {
          id: read.id,
          first: first.file,
          second: read.file,
        }),
      );
    }
    swiplets.set(read.id, read);
  }
  const image =
    source.image === undefined
      ? undefined
      : {
          name: path.basename(source.image),
          data: fs.readFileSync(path.resolve(root, source.image)),
        };
  const located = (name: string) =>
    source.prefix ? new URL(name, source.prefix).href : name;
  const dist = path.join(root, "dist");
  fs.rmSync(dist, { recursive: true, force: true });
  fs.mkdirSync(dist, { recursive: true });
  const entries = [...swiplets.values()].map(({ id, manifest, data }) => {
    const name = `${id}.swiplet`;
    fs.writeFileSync(path.join(dist, name), data);
    return {
      ...manifest,
      url: located(name),
      size: data.length,
      sha256: createHash("sha256").update(data).digest("hex"),
    };
  });
  if (image) {
    fs.writeFileSync(path.join(dist, image.name), image.data);
  }
  const index = {
    ...(source.name ? { name: source.name } : {}),
    ...(Object.keys(source.localizedNames).length > 0
      ? { localizedNames: source.localizedNames }
      : {}),
    ...(source.icon
      ? {
          icon: {
            symbol: source.icon.symbol,
            ...(source.icon.color ? { color: source.icon.color } : {}),
            ...(source.icon.symbolColor
              ? { symbolColor: source.icon.symbolColor }
              : {}),
          },
        }
      : {}),
    ...(image ? { image: located(image.name) } : {}),
    swiplets: entries,
  };
  fs.writeFileSync(
    path.join(dist, "swiplets.json"),
    `${JSON.stringify(index, null, 2)}\n`,
  );
  console.log(path.relative(process.cwd(), path.join(dist, "swiplets.json")));
}

export const source = defineCommand({
  name: "source",
  use: "source [SWIPLET...]",
  short: tr("Collect the built swiplets into dist/ with a swiplets.json"),
  args: [0, Infinity],
  run: ({ args }) => sourceCommand(process.cwd(), args),
});
