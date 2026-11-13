import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";

import { strFromU8, unzipSync } from "fflate";

import { tr } from "../../../i18n";
import { idPattern } from "../../../project";
import { defineCommand } from "../../command";

function loadSource(root) {
  const manifest = JSON.parse(
    fs.readFileSync(path.join(root, "package.json"), "utf8"),
  );
  const config = manifest.source ?? {};
  for (const field of ["name", "image", "url"]) {
    if (config[field] !== undefined && typeof config[field] !== "string") {
      throw new Error(
        tr('"{field}" in package.json must be a string.', {
          field: `source.${field}`,
        }),
      );
    }
  }
  const localizedNames = config.localizedNames ?? {};
  if (
    localizedNames === null ||
    typeof localizedNames !== "object" ||
    !Object.values(localizedNames).every((name) => typeof name === "string")
  ) {
    throw new Error(
      tr(
        '"source.localizedNames" in package.json must map languages to names.',
      ),
    );
  }
  if (
    config.icon !== undefined &&
    (typeof config.icon?.symbol !== "string" ||
      (config.icon.color !== undefined &&
        typeof config.icon.color !== "string"))
  ) {
    throw new Error(
      tr(
        '"source.icon" in package.json must have a symbol and may have a color.',
      ),
    );
  }
  if (
    config.icon?.symbolColor !== undefined &&
    typeof config.icon.symbolColor !== "string"
  ) {
    throw new Error(
      tr('"{field}" in package.json must be a string.', {
        field: "source.icon.symbolColor",
      }),
    );
  }
  let prefix;
  if (config.url !== undefined) {
    try {
      prefix = new URL(
        config.url.endsWith("/") ? config.url : `${config.url}/`,
      );
    } catch {
      throw new Error(
        tr('"source.url" in package.json isn\'t an absolute URL.'),
      );
    }
  }
  const workspaces = Array.isArray(manifest.workspaces)
    ? manifest.workspaces
    : (manifest.workspaces?.packages ?? []);
  return { root, config, localizedNames, prefix, workspaces };
}

function workspaceSwiplets(source) {
  const files = [];
  for (const pattern of source.workspaces) {
    for (const directory of fs.globSync(pattern, { cwd: source.root }).sort()) {
      const project = path.join(source.root, directory, "package.json");
      if (!fs.existsSync(project)) {
        continue;
      }
      const id = JSON.parse(fs.readFileSync(project, "utf8")).swiplet?.id;
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

function readSwiplet(file) {
  const data = fs.readFileSync(file);
  let entry;
  try {
    entry = unzipSync(data, {
      filter: ({ name }) => name === "manifest.json",
    })["manifest.json"];
  } catch (error) {
    throw new Error(`${file}: ${error.message}`);
  }
  if (entry === undefined) {
    throw new Error(`${file} has no manifest.json`);
  }
  const manifest = JSON.parse(strFromU8(entry));
  if (typeof manifest.id !== "string" || !idPattern.test(manifest.id)) {
    throw new Error(`${file} has no valid swiplet id in its manifest`);
  }
  return { file, manifest, data };
}

function sourceCommand(root, given) {
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
  const swiplets = new Map();
  for (const read of files.map(readSwiplet)) {
    const { id } = read.manifest;
    if (swiplets.has(id)) {
      throw new Error(
        tr("{id} is both {first} and {second}", {
          id,
          first: swiplets.get(id).file,
          second: read.file,
        }),
      );
    }
    swiplets.set(id, read);
  }
  const image =
    source.config.image === undefined
      ? undefined
      : {
          name: path.basename(source.config.image),
          data: fs.readFileSync(path.resolve(root, source.config.image)),
        };
  const located = (name) =>
    source.prefix ? new URL(name, source.prefix).href : name;
  const dist = path.join(root, "dist");
  fs.rmSync(dist, { recursive: true, force: true });
  fs.mkdirSync(dist, { recursive: true });
  const entries = [...swiplets.keys()].map((id) => {
    const { manifest, data } = swiplets.get(id);
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
    ...(source.config.name ? { name: source.config.name } : {}),
    ...(Object.keys(source.localizedNames).length > 0
      ? { localizedNames: source.localizedNames }
      : {}),
    ...(source.config.icon
      ? {
          icon: {
            symbol: source.config.icon.symbol,
            ...(source.config.icon.color
              ? { color: source.config.icon.color }
              : {}),
            ...(source.config.icon.symbolColor
              ? { symbolColor: source.config.icon.symbolColor }
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
