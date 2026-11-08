import { createRequire } from "node:module";
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

import { fromJson, toJson } from "@bufbuild/protobuf";
import { ValueSchema, timestampFromMs } from "@bufbuild/protobuf/wkt";

import { UsageError, unauthenticated } from "./client";
import { attach } from "./connection";
import { tr } from "./i18n";
import { Ignorer, relativePath } from "./ignore";
import {
  columns,
  dateTime,
  printLog,
  printScriptError,
  serviceEnabledText,
  serviceStatusText,
  serviceText,
} from "./output";
import { AvailabilityChecker } from "./availability";
import { buildProject, loadProject } from "./project";
import { LogLevel } from "./proto/common_pb";
import { ServiceStatus } from "./proto/swiplets_pb";

const uploadChunk = 256 << 10;
const rebuildDelay = 300;
const logReconnect = 2000;

const { version } = createRequire(import.meta.url)("../package.json");

const readStdin = () => fs.readFileSync(0);

const indented = (value) =>
  JSON.stringify(value, null, 2).replaceAll("\n", "\n  ");

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

const session = (context, run) => attach(context).then(run);

export function printVersion() {
  console.log(
    "swipt",
    version,
    `node ${process.versions.node}`,
    `${process.platform}/${process.arch}`,
  );
}

export const logs = (context) =>
  session(context, async (client) => {
    const { options } = context;
    const levels = ["error", "warn", "info", "debug"];
    const index = levels.indexOf(options.level);
    if (index < 0) {
      throw new UsageError(tr("--level is error, warn, info or debug"));
    }
    const request: Record<string, any> = {
      follow: options.follow,
      tail: options.tail ?? 100,
      swipletId: options.swiplet ?? "",
      level: index + 1,
    };
    if (options.since > 0) {
      request.since = timestampFromMs(Date.now() - options.since);
      if (options.tail === undefined) {
        request.tail = 0xffffffff;
      }
    }
    for await (const response of client.logs.readLogs(request, {
      signal: context.signal,
    })) {
      printLog(response.entry, true);
    }
  });

export const stop = (context) =>
  session(context, async (client) => {
    await client.system.shutdown({}, { signal: context.signal });
    console.log(tr("stopped"));
  });

function readInput(value) {
  if (value === undefined || value === "") {
    return undefined;
  }
  let data = value;
  try {
    if (value === "-") {
      data = readStdin().toString();
    } else if (value.startsWith("@")) {
      data = fs.readFileSync(value.slice(1), "utf8");
    }
  } catch (error) {
    throw new Error(`read --input: ${error.message}`);
  }
  try {
    return fromJson(ValueSchema, JSON.parse(data));
  } catch (error) {
    throw new UsageError(
      tr("--input is not JSON: {error}", { error: error.message }),
    );
  }
}

const notInstalled = (id) =>
  new Error(tr("there is no swiplet {id}", { id: JSON.stringify(id) }));

export const list = (context) =>
  session(context, async (client) => {
    const { swiplets } = await client.swiplets.listSwiplets(
      {},
      { signal: context.signal },
    );
    const rows = swiplets.map((swiplet) => {
      let service = "";
      let status = "";
      if (swiplet.service) {
        service = serviceEnabledText(swiplet.service.enabled);
        status = serviceStatusText(swiplet.service.status);
        if (swiplet.service.error) {
          status += `: ${swiplet.service.error.name}: ${swiplet.service.error.message}`;
        }
      }
      return [
        swiplet.id,
        swiplet.title,
        swiplet.version,
        swiplet.actions.map((action) => action.name).join(","),
        service,
        status,
      ];
    });
    console.log(
      columns([
        [
          tr("ID"),
          tr("TITLE"),
          tr("VERSION"),
          tr("ACTIONS"),
          tr("SERVICE"),
          tr("STATUS"),
        ],
        ...rows,
      ]),
    );
  });

export const show = (context) =>
  session(context, async (client) => {
    const { swiplets } = await client.swiplets.listSwiplets(
      {},
      { signal: context.signal },
    );
    const swiplet = swiplets.find((it) => it.id === context.args[0]);
    if (!swiplet) {
      throw notInstalled(context.args[0]);
    }
    console.log(swiplet.id, JSON.stringify(swiplet.title), swiplet.version);
    if (swiplet.hasUi) {
      console.log(tr("ui: yes"));
    }
    if (swiplet.service) {
      console.log(
        tr("service: {status}", {
          status: serviceText(swiplet.id, swiplet.service),
        }),
      );
    }
    for (const action of swiplet.actions) {
      console.log();
      console.log(
        action.exposedToShortcuts
          ? tr("action {name} {title} (Shortcuts)", {
              name: action.name,
              title: JSON.stringify(action.title),
            })
          : tr("action {name} {title}", {
              name: action.name,
              title: JSON.stringify(action.title),
            }),
      );
      if (action.description !== "") {
        console.log(`  ${action.description}`);
      }
      if (action.inputSchema) {
        console.log(
          `  ${tr("input: {schema}", { schema: indented(action.inputSchema) })}`,
        );
      }
      if (action.outputSchema) {
        console.log(
          `  ${tr("output: {schema}", { schema: indented(action.outputSchema) })}`,
        );
      }
    }
  });

export const call = (context) =>
  session(context, async (client) => {
    const [id, action] = context.args;
    const input = readInput(context.options.input);
    let result;
    for await (const response of client.swiplets.callAction(
      { swipletId: id, action, input },
      { signal: context.signal },
    )) {
      if (response.part.case === "log") {
        printLog(response.part.value, false);
      } else if (response.part.case === "result") {
        result = response.part.value;
      }
    }
    if (result?.outcome.case === "output") {
      console.log(
        JSON.stringify(toJson(ValueSchema, result.outcome.value), null, 2),
      );
      return;
    }
    if (result?.outcome.case === "error") {
      throw printScriptError(result.outcome.value);
    }
    throw new Error("the server sent no result");
  });

async function watchServices(client, id, signal) {
  for await (const response of client.swiplets.watchServices(
    { swipletId: id },
    { signal },
  )) {
    console.log(
      `${response.swipletId}: ${serviceText(response.swipletId, response.service)}`,
    );
  }
}

export const setService = (enabled) => (context) =>
  session(context, async (client) => {
    const id = context.args[0];
    const response = await client.swiplets.setServiceEnabled(
      { swipletId: id, enabled },
      { signal: context.signal },
    );
    if (context.options.watch) {
      await watchServices(client, id, context.signal);
      return;
    }
    console.log(`${id}: ${serviceText(id, response.service)}`);
  });

export const serviceWatch = (context) =>
  session(context, (client) =>
    watchServices(client, context.args[0] ?? "", context.signal),
  );

export const remove = (context) =>
  session(context, async (client) => {
    await client.swiplets.removeSwiplet(
      { swipletId: context.args[0] },
      { signal: context.signal },
    );
    console.log(tr("removed {name}", { name: context.args[0] }));
  });

const isLocal = (name) => {
  const normalized = path.posix.normalize(name);
  return (
    !path.posix.isAbsolute(normalized) &&
    normalized !== ".." &&
    !normalized.startsWith("../")
  );
};

class FileSink {
  declare root: string;
  files = new Map();
  saved = 0;

  constructor(root) {
    this.root = root;
  }

  target(name) {
    if (!isLocal(name)) {
      throw new Error(
        `the server sent the path ${name}, which leaves the output directory`,
      );
    }
    return path.join(this.root, ...name.split("/"));
  }

  begin(name, directory) {
    const target = this.target(name);
    if (directory) {
      fs.mkdirSync(target, { recursive: true });
      return;
    }
    fs.mkdirSync(path.dirname(target), { recursive: true });
    const previous = this.files.get(name);
    if (previous !== undefined) {
      fs.closeSync(previous);
    }
    this.files.set(name, fs.openSync(target, "w"));
    this.saved++;
  }

  write(name, offset, data) {
    const file = this.files.get(name);
    if (file === undefined) {
      throw new Error(`the server sent data for ${name} before announcing it`);
    }
    fs.writeSync(file, data, 0, data.length, Number(offset));
  }

  close() {
    for (const file of this.files.values()) {
      fs.closeSync(file);
    }
  }
}

const printSaved = (target, count) =>
  console.error(tr("saved {count} file(s) to {path}", { count, path: target }));

const dataCommands = (target) => ({
  list: (context) =>
    session(context, async (client) => {
      const {
        id,
        args: [directory = ""],
      } = target(context.args);
      const { files } = await client.swiplets.listDataFiles(
        {
          swipletId: id,
          path: directory,
          recursive: context.options.recursive,
        },
        { signal: context.signal },
      );
      const rows = files.map((file) => [
        file.directory ? "-" : String(file.size),
        dateTime(file.modified),
        file.directory ? `${file.path}/` : file.path,
      ]);
      if (rows.length > 0) {
        console.log(columns(rows));
      }
    }),
  read: (context) =>
    session(context, async (client) => {
      const {
        id,
        wholeDirectory,
        subdirectory,
        args: [given],
      } = target(context.args);
      const output = context.options.output ?? "";
      const requested = path.posix
        .normalize(`/${given}`)
        .replace(/^\/+|\/+$/g, "");
      const printing = output === "" || output === "-";
      const directory = () =>
        new UsageError(
          requested === ""
            ? wholeDirectory(JSON.stringify(given))
            : subdirectory(JSON.stringify(requested)),
        );
      const relativeTo = (name) =>
        name.slice(requested.length).replace(/^\//, "");
      let sink;
      let single;
      const contents = [];
      try {
        for await (const response of client.swiplets.readDataFile(
          { swipletId: id, path: given },
          { signal: context.signal },
        )) {
          if (response.part.case === "file") {
            const file = response.part.value;
            if (file.path === requested && !file.directory) {
              single = file;
              continue;
            }
            if (!sink) {
              if (printing) {
                throw directory();
              }
              sink = new FileSink(output);
            }
            sink.begin(relativeTo(file.path), file.directory);
          } else if (response.part.case === "chunk") {
            const chunk = response.part.value;
            if (single && chunk.path === single.path) {
              contents.push(chunk.data);
              continue;
            }
            if (!sink) {
              throw new Error(
                `the server sent data for ${chunk.path} before announcing it`,
              );
            }
            sink.write(relativeTo(chunk.path), chunk.offset, chunk.data);
          }
        }
      } finally {
        sink?.close();
      }
      if (sink) {
        printSaved(sink.root, sink.saved);
        return;
      }
      if (!single) {
        if (printing) {
          throw directory();
        }
        fs.mkdirSync(output, { recursive: true });
        printSaved(output, 0);
        return;
      }
      const data = Buffer.concat(contents);
      if (printing) {
        process.stdout.write(data);
        return;
      }
      let destination = output;
      if (
        fs.existsSync(destination) &&
        fs.statSync(destination).isDirectory()
      ) {
        destination = path.join(destination, path.posix.basename(single.path));
      }
      fs.writeFileSync(destination, data);
      printSaved(destination, 1);
    }),
  write: (context) =>
    session(context, async (client) => {
      const {
        id,
        args: [name, file],
      } = target(context.args);
      const data =
        file === undefined || file === "-"
          ? readStdin()
          : fs.readFileSync(file);
      await client.swiplets.writeDataFile(
        (async function* () {
          yield {
            part: { case: "header", value: { swipletId: id, path: name } },
          };
          for (let offset = 0; offset < data.length; offset += uploadChunk) {
            yield {
              part: {
                case: "data",
                value: data.subarray(offset, offset + uploadChunk),
              },
            };
          }
        })(),
        { signal: context.signal },
      );
    }),
  remove: (context) =>
    session(context, async (client) => {
      const {
        id,
        args: [name],
      } = target(context.args);
      await client.swiplets.removeDataFile(
        { swipletId: id, path: name, recursive: context.options.recursive },
        { signal: context.signal },
      );
    }),
  clear: (context) =>
    session(context, async (client) => {
      const { id } = target(context.args);
      const { files } = await client.swiplets.listDataFiles(
        { swipletId: id },
        { signal: context.signal },
      );
      for (const file of files) {
        await client.swiplets.removeDataFile(
          { swipletId: id, path: file.path, recursive: true },
          { signal: context.signal },
        );
      }
    }),
});

export const swipletData = dataCommands(([id, ...args]) => ({
  id,
  wholeDirectory: (name) =>
    tr(
      "{path} names the data directory of {swiplet}; save it with --output DIRECTORY",
      { path: name, swiplet: id },
    ),
  subdirectory: (name) =>
    tr(
      "{path} is a directory in the data directory of {swiplet}; save it with --output DIRECTORY",
      { path: name, swiplet: id },
    ),
  args,
}));

export const replData = dataCommands((args) => ({
  id: "@repl",
  wholeDirectory: (name) =>
    tr(
      "{path} names the data directory of the REPL; save it with --output DIRECTORY",
      { path: name },
    ),
  subdirectory: (name) =>
    tr(
      "{path} is a directory in the data directory of the REPL; save it with --output DIRECTORY",
      { path: name },
    ),
  args,
}));

async function deploy(client, archive, signal) {
  const data = fs.readFileSync(archive);
  return client.swiplets.deploySwiplet(
    (async function* () {
      for (
        let offset = 0;
        offset < data.length || offset === 0;
        offset += uploadChunk
      ) {
        yield { chunk: data.subarray(offset, offset + uploadChunk) };
      }
    })(),
    { signal },
  );
}

const printDeployed = (deployed, toStdout) =>
  (toStdout ? console.log : console.error)(
    deployed.replaced
      ? tr("replaced {id} {version}", {
          id: deployed.swiplet.id,
          version: deployed.swiplet.version,
        })
      : tr("installed {id} {version}", {
          id: deployed.swiplet.id,
          version: deployed.swiplet.version,
        }),
  );

export async function install(context) {
  const [archive] = context.args;
  if (!fs.existsSync(archive)) {
    throw new Error(tr("there is no archive at {path}", { path: archive }));
  }
  await session(context, async (client) =>
    printDeployed(await deploy(client, archive, context.signal), true),
  );
}

export async function deployProject(context) {
  const { archive } = await buildProject(path.resolve(context.args[0] ?? "."));
  await session(context, async (client) =>
    printDeployed(await deploy(client, archive, context.signal), true),
  );
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

export async function develop(context) {
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

const isFile = (name) => {
  try {
    return fs.statSync(name).isFile();
  } catch {
    return false;
  }
};

export const isProgram = (first) => first === "-" || isFile(first);

function readScript([first, ...args]) {
  if (first === undefined || first === "-") {
    return {
      code: readStdin().toString(),
      args,
      program: true,
      file: "input.js",
    };
  }
  if (isFile(first)) {
    return {
      code: fs.readFileSync(first, "utf8"),
      args,
      program: true,
      file: first,
    };
  }
  if (args.length > 0) {
    throw new UsageError(
      tr("{argument} is not a file, and CODE takes no ARG", {
        argument: JSON.stringify(first),
      }),
    );
  }
  return { code: first, args, program: false, file: "input.js" };
}

class ImageSink {
  declare output: string | undefined;
  directory = "";

  constructor(output) {
    this.output = output;
  }

  save(data) {
    if (this.directory === "") {
      if (this.output !== undefined && this.output !== "") {
        this.directory = path.resolve(this.output);
        fs.mkdirSync(this.directory, { recursive: true });
      } else {
        this.directory = fs.mkdtempSync(path.join(os.tmpdir(), "swipt-repl-"));
      }
    }
    const name = path.join(
      this.directory,
      `image-${crypto.randomBytes(5).toString("hex")}.png`,
    );
    fs.writeFileSync(name, data);
    return name;
  }
}

export const api = (context) =>
  session(context, async (client) => {
    process.stdout.write(
      (await client.repl.getDeclarations({}, { signal: context.signal }))
        .typescript,
    );
  });

export const reset = (context) =>
  session(context, (client) =>
    client.repl.reset({}, { signal: context.signal }),
  );

export async function run(context) {
  const { args, options, signal } = context;
  const script = readScript(args);
  await session(context, async (client) => {
    const images = new ImageSink(options.output);
    let failure;
    for await (const response of client.repl.execute(
      { code: script.code, args: script.args, program: script.program },
      { signal },
    )) {
      switch (response.part.case) {
        case "console": {
          const output = response.part.value;
          (output.level === LogLevel.ERROR || output.level === LogLevel.WARN
            ? console.error
            : console.log)(output.text);
          break;
        }
        case "value":
          console.log(response.part.value.text);
          break;
        case "image":
          console.log(`[image: ${images.save(response.part.value)}]`);
          break;
        case "error":
          failure = response.part.value;
          break;
      }
    }
    if (failure) {
      throw printScriptError({
        ...failure,
        stack: failure.stack.map((frame) =>
          frame.file === "input.js" ? { ...frame, file: script.file } : frame,
        ),
      });
    }
  });
}
