import fs from "node:fs";
import path from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

import { AvailabilityChecker } from "../../../availability";
import { unauthenticated } from "../../../client";
import { attach } from "../../../connection";
import { tr } from "../../../i18n";
import { Ignorer, relativePath } from "../../../ignore";
import { printLog, serviceText } from "../../../output";
import { buildProject, loadProject } from "../../../project";
import { ServiceStatus } from "../../../proto/swiplets_pb";
import { defineCommand } from "../../command";
import { deploy, printDeployed } from "../../deploy";
import { deviceOption } from "../flags";

const rebuildDelay = 300;
const logReconnect = 2000;

function elapsed(since) {
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

async function follow(signal, fatal, run) {
  let reported;
  while (!signal.aborted) {
    try {
      await run();
    } catch (error) {
      if (unauthenticated(error)) {
        fatal(error);
        return;
      }
      if (!signal.aborted && error.message !== reported) {
        reported = error.message;
        console.error(tr("error: {message}", { message: error.message }));
      }
    }
    try {
      await sleep(logReconnect, undefined, { signal });
    } catch {
      return;
    }
  }
}

async function developProject(context) {
  const { signal, printError } = context;
  const project = loadProject(path.resolve(context.args[0] ?? "."));
  const root = project.root;
  const client = await attach(context);
  const controller = new AbortController();
  const inner = AbortSignal.any([signal, controller.signal]);
  let fatalReject;
  const fatal = new Promise((_, reject) => {
    fatalReject = reject;
  });
  follow(inner, fatalReject, async () => {
    for await (const response of client.swiplets.watchServices(
      {},
      { signal: inner },
    )) {
      if (
        response.swipletId === project.config.id &&
        response.service?.status === ServiceStatus.FAILED
      ) {
        console.error(
          `${response.swipletId}: ${serviceText(response.swipletId, response.service)}`,
        );
      }
    }
  });
  if (!context.options["no-logs"]) {
    follow(inner, fatalReject, async () => {
      for await (const response of client.logs.readLogs(
        { swipletId: project.config.id, follow: true },
        { signal: inner },
      )) {
        printLog(response.entry, false);
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
        console.error(tr("error: {message}", { message: error.message }));
      }
      return;
    }
    if (built.digest === deployed) {
      console.error(
        tr(
          "built in {elapsed}; it came out as deployed, so nothing was deployed",
          { elapsed: elapsed(started) },
        ),
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
      tr("built and deployed in {elapsed}; watching {directory}", {
        elapsed: elapsed(started),
        directory: root,
      }),
    );
  };
  const watch = () =>
    new Promise<void>((resolve, reject) => {
      let ignore = new Ignorer(root);
      let timer;
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
    await cycle();
    await Promise.race([watch(), fatal]);
  } finally {
    controller.abort();
    checker.close();
  }
}

export const develop = defineCommand({
  name: "develop",
  use: "develop [PROJECT]",
  short: tr("Build, install and follow a swiplet project as it changes"),
  args: [0, 1],
  options: {
    ...deviceOption,
    "no-logs": { type: "boolean", help: tr("without logs") },
  },
  run: developProject,
});
