import fs from "node:fs";
import path from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

import { create } from "@bufbuild/protobuf";

import { AvailabilityChecker } from "../../../availability";
import { unauthenticated } from "../../../client";
import { attach } from "../../../connection";
import { t } from "../../../i18n";
import { Ignorer, relativePath } from "../../../ignore";
import { errorMessage, printLog, serviceText } from "../../../output";
import { buildProject, loadProject } from "../../../project";
import { LogEntrySchema } from "../../../proto/common_pb";
import { ServiceStatus } from "../../../proto/swiplets_pb";
import { type Context, defineCommand } from "../../command";
import { deploy, printDeployed } from "../../deploy";
import { deviceOption } from "../flags";

const rebuildDelay = 300;
const logReconnect = 2000;

function elapsed(since: number) {
  const ms = Date.now() - since;
  if (ms < 1000) {
    return `${ms}ms`;
  }
  const seconds = ms / 1000;
  if (seconds < 60) {
    return `${Number(seconds.toFixed(3))}s`;
  }
  return `${Math.floor(seconds / 60)}m${Number((seconds % 60).toFixed(3))}s`;
}

async function follow(
  signal: AbortSignal,
  fatal: (error: unknown) => void,
  run: () => Promise<void>,
) {
  let reported: string | undefined;
  while (!signal.aborted) {
    try {
      await run();
    } catch (error) {
      if (unauthenticated(error)) {
        fatal(error);
        return;
      }
      const message = errorMessage(error);
      if (!signal.aborted && message !== reported) {
        reported = message;
        console.error(t("error", { message }));
      }
    }
    try {
      await sleep(logReconnect, undefined, { signal });
    } catch {
      return;
    }
  }
}

async function developProject(
  context: Context<{ device?: string; "no-logs"?: boolean }>,
) {
  const { signal, printError } = context;
  const project = loadProject(path.resolve(context.args[0] ?? "."));
  const root = project.root;
  const client = await attach(context);
  const controller = new AbortController();
  const inner = AbortSignal.any([signal, controller.signal]);
  const fatal = Promise.withResolvers<never>();
  follow(inner, fatal.reject, async () => {
    for await (const response of client.swiplets.watchServices(
      {},
      { signal: inner },
    )) {
      if (
        response.swipletId === project.config.id &&
        response.service?.status === ServiceStatus.FAILED
      ) {
        console.error(
          `${response.swipletId}: ${serviceText(response.service)}`,
        );
      }
    }
  });
  if (!context.options["no-logs"]) {
    follow(inner, fatal.reject, async () => {
      for await (const response of client.logs.readLogs(
        { swipletId: project.config.id, follow: true },
        { signal: inner },
      )) {
        printLog(response.entry ?? create(LogEntrySchema), false);
      }
    });
  }
  let deployed = "";
  const checker = new AvailabilityChecker();
  const cycle = async () => {
    const started = Date.now();
    let built;
    try {
      built = await buildProject(root, checker);
    } catch (error) {
      if (!inner.aborted) {
        printError(error);
      }
      return;
    }
    if (built.digest === deployed) {
      console.error(
        t("commands.project.develop.unchanged", {
          elapsed: elapsed(started),
        }),
      );
      return;
    }
    let result;
    try {
      result = await deploy(client, built.archive, inner);
    } catch (error) {
      if (unauthenticated(error)) {
        throw error;
      }
      if (!inner.aborted) {
        printError(error, "deploy");
      }
      return;
    }
    deployed = built.digest;
    printDeployed(result, false);
    console.error(
      t("commands.project.develop.deployed", {
        elapsed: elapsed(started),
        directory: root,
      }),
    );
  };
  const watch = () =>
    new Promise<void>((resolve, reject) => {
      if (inner.aborted) {
        resolve();
        return;
      }
      let ignore = new Ignorer(root);
      let timer: NodeJS.Timeout | undefined;
      const watcher = fs.watch(root, { recursive: true }, (_, filename) => {
        if (filename === null) {
          return;
        }
        const name = path.join(root, filename);
        if (path.basename(name) === ".gitignore") {
          ignore = new Ignorer(root);
        }
        let isDirectory = false;
        try {
          isDirectory = fs.statSync(name).isDirectory();
        } catch {
          isDirectory = false;
        }
        const relative = relativePath(root, name);
        if (relative === "" || ignore.ignored(relative, isDirectory)) {
          return;
        }
        clearTimeout(timer);
        timer = setTimeout(() => cycle().catch(reject), rebuildDelay);
      });
      watcher.on("error", (error) => printError(error, "watch the project"));
      inner.addEventListener(
        "abort",
        () => {
          clearTimeout(timer);
          watcher.close();
          resolve();
        },
        { once: true },
      );
    });
  try {
    await Promise.race([cycle().then(watch), fatal.promise]);
  } finally {
    controller.abort();
    checker.close();
  }
}

export const develop = defineCommand({
  name: "develop",
  use: "develop [PROJECT]",
  short: t("commands.project.develop.description"),
  args: [0, 1],
  options: {
    ...deviceOption,
    "no-logs": { type: "boolean", help: t("commands.project.develop.quiet") },
  },
  run: developProject,
});
