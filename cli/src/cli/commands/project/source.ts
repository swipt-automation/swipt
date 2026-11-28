import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";

import { errorMessage } from "../../../errors";
import { t } from "../../../i18n";
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

async function sourceCommand(root: string, given: string[]) {
  const [{ strFromU8, unzipSync }, { idPattern, parseIcon, stringField }] =
    await Promise.all([import("fflate"), import("../../../project")]);

  const loadSource = () => {
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
  };

  const workspaceSwiplets = (sourceInfo: ReturnType<typeof loadSource>) => {
    const files: string[] = [];
    for (const pattern of sourceInfo.workspaces) {
      for (const directory of fs
        .globSync(pattern, { cwd: sourceInfo.root })
        .sort()) {
        const project = path.join(sourceInfo.root, directory, "package.json");
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
        const built = path.join(
          sourceInfo.root,
          directory,
          "dist",
          `${id}.swiplet`,
        );
        if (!fs.existsSync(built)) {
          throw new Error(
            t("commands.project.source.notBuilt", {
              file: path.relative(sourceInfo.root, built),
              directory,
            }),
          );
        }
        files.push(built);
      }
    }
    return files;
  };

  const readSwiplet = (file: string) => {
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
  };

  const sourceInfo = loadSource();
  const files =
    given.length > 0
      ? given.map((file) => path.resolve(file))
      : workspaceSwiplets(sourceInfo);
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
    sourceInfo.image === undefined
      ? undefined
      : {
          name: path.basename(sourceInfo.image),
          data: fs.readFileSync(path.resolve(root, sourceInfo.image)),
        };
  const located = (name: string) =>
    sourceInfo.prefix ? new URL(name, sourceInfo.prefix).href : name;
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
    ...(sourceInfo.name ? { name: sourceInfo.name } : {}),
    ...(Object.keys(sourceInfo.localizedNames).length > 0
      ? { localizedNames: sourceInfo.localizedNames }
      : {}),
    ...(sourceInfo.icon ? { icon: sourceInfo.icon } : {}),
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
