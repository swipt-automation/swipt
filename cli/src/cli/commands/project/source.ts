import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";

import { strFromU8, unzipSync } from "fflate";

import { t } from "../../../i18n";
import { errorMessage } from "../../../output";
import { idPattern, parseIcon, stringField } from "../../../project";
import { defineCommand } from "../../command";

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const isStringRecord = (value: unknown): value is Record<string, string> =>
  isRecord(value) &&
  Object.values(value).every((name) => typeof name === "string");

function workspacePatterns(workspaces: unknown) {
  const [field, patterns] = Array.isArray(workspaces)
    ? ["workspaces", workspaces]
    : isRecord(workspaces) && Array.isArray(workspaces.packages)
      ? ["workspaces.packages", workspaces.packages]
      : ["", []];
  return patterns.map((pattern: unknown, index) => {
    if (typeof pattern !== "string") {
      throw new Error(
        t("project.invalidString", { field: `${field}[${index}]` }),
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
    throw new Error(t("commands.project.source.invalidLocalizedNames"));
  }
  const icon = parseIcon(config.icon, {
    invalidIcon: t("commands.project.source.invalidIcon"),
    invalidAndroidSymbol: (symbol) =>
      t("commands.project.source.invalidAndroidSymbol", { symbol }),
  });
  let prefix: URL | undefined;
  if (url !== undefined) {
    try {
      prefix = new URL(url.endsWith("/") ? url : `${url}/`);
    } catch {
      throw new Error(t("commands.project.source.invalidURL"));
    }
  }
  const workspaces = workspacePatterns(manifest.workspaces);
  return {
    root,
    name,
    image,
    localizedNames,
    icon,
    prefix,
    workspaces,
  };
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
          t("commands.project.source.notBuilt", {
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
    throw new Error(t("commands.project.source.noWorkspaces"));
  }
  const swiplets = new Map<string, ReturnType<typeof readSwiplet>>();
  for (const read of files.map(readSwiplet)) {
    const first = swiplets.get(read.id);
    if (first !== undefined) {
      throw new Error(
        t("commands.project.source.duplicateID", {
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
    ...(source.icon ? { icon: source.icon } : {}),
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
  short: t("commands.project.source.description"),
  args: [0, Infinity],
  run: ({ args }) => sourceCommand(process.cwd(), args),
});
