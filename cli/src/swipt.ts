#!/usr/bin/env node
import path from "node:path";
import { parseArgs } from "node:util";

import { Code, RpcError, UsageError } from "./client";
import * as connection from "./connection";
import * as device from "./device";
import { tr } from "./i18n";
import { FalseResult } from "./output";
import { buildProject, createCommand, sourceCommand } from "./project";
import * as sideload from "./sideload";

interface Option {
  type: "string" | "boolean";
  short?: string;
  value?: string;
  help: string;
}

type Options = Record<string, Option>;

interface Command {
  name: string;
  use?: string;
  short: string;
  description?: string[];
  args?: [least: number, most: number];
  options?: Options;
  commands?: Command[];
  passthrough?: (word: string) => boolean;
  parse?: (values: Record<string, any>) => Record<string, any>;
  run?: (context: any) => unknown;
}

const globalOptions: Options = {
  help: { type: "boolean", short: "h", help: tr("help") },
};

const deviceOption: Options = {
  device: {
    type: "string",
    short: "d",
    value: "NAME|URL",
    help: tr("the device to use"),
  },
};

const sideloadOptions: Options = {
  device: {
    type: "string",
    short: "d",
    value: "NAME|UDID",
    help: tr("the device to use"),
  },
  team: {
    type: "string",
    value: "ID",
    help: tr("team ID, required if more than one"),
  },
};

const passcodeOptions = (help): Options => ({
  passcode: { type: "string", value: "CODE", help },
  "passcode-type": {
    type: "string",
    value: "TYPE",
    help: tr("pin (4 or 6 digits, default), numeric or alphanumeric"),
  },
});

const nightlyOption: Options = {
  nightly: { type: "boolean", help: tr("use nightly build") },
};

const setupStepOptions: Options = {
  "no-setup-pair": { type: "boolean", help: tr("do not pair Swipt") },
  "no-setup-api": {
    type: "boolean",
    help: tr("do not publish the API to the local network"),
  },
};

const setupOptions: Options = {
  "no-setup": {
    type: "boolean",
    help: tr("do not pair Swipt, trust its developer or publish its API"),
  },
  ...setupStepOptions,
};

const durationUnits = {
  ns: 1e-6,
  us: 1e-3,
  µs: 1e-3,
  ms: 1,
  s: 1000,
  m: 60_000,
  h: 3_600_000,
};

function parseDuration(text, option) {
  if (text === undefined) {
    return 0;
  }
  if (/^-?\d+$/.test(text) && Number(text) === 0) {
    return 0;
  }
  let total = 0;
  let rest = text;
  while (rest !== "") {
    const match = /^(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|ms|s|m|h)/.exec(rest);
    if (!match) {
      throw new UsageError(
        tr(
          "invalid argument {value} for {option}: it is a duration such as 30s or 1m30s",
          { value: JSON.stringify(text), option },
        ),
      );
    }
    total += Number(match[1]) * durationUnits[match[2]];
    rest = rest.slice(match[0].length);
  }
  return total;
}

function parseCount(text, option) {
  if (text === undefined) {
    return undefined;
  }
  if (!/^\d+$/.test(text) || Number(text) > 0xffffffff) {
    throw new UsageError(
      tr("invalid argument {value} for {option}: it is a number", {
        value: JSON.stringify(text),
        option,
      }),
    );
  }
  return Number(text);
}

const dataCommands = (operand, files): Command => {
  const before = operand === "" ? 0 : 1;
  return {
    name: "data",
    short:
      operand === ""
        ? tr("Manage the files in the data directory of the REPL")
        : tr("Manage the files in the data directory of a swiplet"),
    commands: [
      {
        name: "list",
        use: `list ${operand}[PATH]`,
        short: tr("List data files"),
        args: [before, before + 1],
        options: {
          recursive: { type: "boolean", help: tr("include subdirectories") },
        },
        run: files.list,
      },
      {
        name: "read",
        use: `read ${operand}PATH`,
        short: tr("Read a data file or directory"),
        args: [before + 1, before + 1],
        options: {
          output: { type: "string", value: "PATH", help: tr("destination") },
        },
        run: files.read,
      },
      {
        name: "write",
        use: `write ${operand}PATH [FILE | -]`,
        short: tr("Write a data file"),
        args: [before + 1, before + 2],
        run: files.write,
      },
      {
        name: "remove",
        use: `remove ${operand}PATH`,
        short: tr("Remove a data file"),
        args: [before + 1, before + 1],
        options: {
          recursive: {
            type: "boolean",
            help: tr("include directory contents"),
          },
        },
        run: files.remove,
      },
      {
        name: "clear",
        use: `clear ${operand}`.trimEnd(),
        short: tr("Remove every data file, keeping the data directory"),
        args: [before, before],
        run: files.clear,
      },
    ],
  };
};

const commands: Command[] = [
  {
    name: "project",
    short: tr("Make, build and deploy swiplet projects"),
    commands: [
      {
        name: "create",
        use: "create DIRECTORY",
        short: tr("Make a swiplet project in a new or empty directory"),
        args: [1, 1],
        run: ({ args }) => createCommand(args[0]),
      },
      {
        name: "build",
        use: "build [PROJECT]",
        short: tr("Bundle the project into dist/<swiplet.id>.swiplet"),
        args: [0, 1],
        run: async ({ args }) => {
          const { archive } = await buildProject(path.resolve(args[0] ?? "."));
          console.log(path.relative(process.cwd(), archive));
        },
      },
      {
        name: "source",
        use: "source [SWIPLET...]",
        short: tr("Collect the built swiplets into dist/ with a swiplets.json"),
        args: [0, Infinity],
        run: ({ args }) => sourceCommand(process.cwd(), args),
      },
      {
        name: "deploy",
        use: "deploy [PROJECT]",
        short: tr("Build the project and install it on the device"),
        args: [0, 1],
        options: deviceOption,
        run: device.deployProject,
      },
      {
        name: "develop",
        use: "develop [PROJECT]",
        short: tr("Build, install and follow a swiplet project as it changes"),
        args: [0, 1],
        options: {
          ...deviceOption,
          "no-logs": { type: "boolean", help: tr("without logs") },
        },
        run: device.develop,
      },
    ],
  },
  {
    name: "device",
    short: tr("Manage the devices and read their logs"),
    commands: [
      {
        name: "list",
        short: tr(
          "List the devices, adding the ones found on the local network",
        ),
        args: [0, 0],
        run: connection.list,
      },
      {
        name: "add",
        use: "add URL",
        short: tr(
          "Add a device by its URL; needed when it is remote or requires a token",
        ),
        args: [1, 1],
        run: connection.add,
      },
      {
        name: "remove",
        use: "remove NAME",
        short: tr("Forget a device"),
        args: [1, 1],
        run: connection.remove,
      },
      {
        name: "stop",
        short: tr("Stop the Swipt server"),
        args: [0, 0],
        options: deviceOption,
        run: device.stop,
      },
      {
        name: "logs",
        short: tr("Print the logs"),
        args: [0, 0],
        options: {
          ...deviceOption,
          follow: { type: "boolean", help: tr("follow new entries") },
          tail: {
            type: "string",
            value: "COUNT",
            help: tr("number of past entries (default 100)"),
          },
          since: { type: "string", value: "DURATION", help: tr("maximum age") },
          level: {
            type: "string",
            value: "LEVEL",
            help: tr("minimum level (default info)"),
          },
          swiplet: { type: "string", value: "ID", help: tr("swiplet ID") },
        },
        parse: (options) => ({
          ...options,
          tail: parseCount(options.tail, "--tail"),
          since: parseDuration(options.since, "--since"),
          level: options.level ?? "info",
        }),
        run: device.logs,
      },
    ],
  },
  {
    name: "swiplet",
    short: tr("Manage the swiplets on the device"),
    options: deviceOption,
    commands: [
      {
        name: "list",
        short: tr("List the swiplets"),
        args: [0, 0],
        run: device.list,
      },
      {
        name: "show",
        use: "show ID",
        short: tr("Show a swiplet"),
        args: [1, 1],
        run: device.show,
      },
      {
        name: "install",
        use: "install ARCHIVE",
        short: tr("Install a swiplet archive"),
        args: [1, 1],
        run: device.install,
      },
      {
        name: "remove",
        use: "remove ID",
        short: tr("Remove a swiplet"),
        args: [1, 1],
        run: device.remove,
      },
      {
        name: "call",
        use: "call ID ACTION",
        short: tr("Call a swiplet action"),
        args: [2, 2],
        options: {
          input: {
            type: "string",
            value: "JSON",
            help: tr("input JSON, @FILE or -"),
          },
        },
        run: device.call,
      },
      {
        name: "service",
        short: tr("Manage swiplet services"),
        commands: [
          {
            name: "enable",
            use: "enable ID",
            short: tr("Turn on a swiplet service"),
            args: [1, 1],
            options: {
              watch: { type: "boolean", help: tr("watch the state") },
            },
            run: device.setService(true),
          },
          {
            name: "disable",
            use: "disable ID",
            short: tr("Turn off a swiplet service"),
            args: [1, 1],
            options: {
              watch: { type: "boolean", help: tr("watch the state") },
            },
            run: device.setService(false),
          },
          {
            name: "watch",
            use: "watch [ID]",
            short: tr("Watch swiplet services"),
            args: [0, 1],
            run: device.serviceWatch,
          },
        ],
      },
      dataCommands("ID ", device.swipletData),
    ],
  },
  {
    name: "repl",
    short: tr("Run JavaScript in the REPL session on the device"),
    options: deviceOption,
    commands: [
      {
        name: "run",
        use: "run [CODE | FILE [ARG...] | - [ARG...]]",
        short: tr("Run a JavaScript expression or program on the device"),
        description: [
          tr(
            "The first argument is FILE when it names an existing file and CODE otherwise; options go before it.",
          ),
          tr(
            "CODE is an expression whose value is printed. FILE, or standard input given as - or no argument, is a program, and only its console output is printed.",
          ),
        ],
        args: [0, Infinity],
        passthrough: device.isProgram,
        options: {
          output: {
            type: "string",
            value: "DIRECTORY",
            help: tr("image directory (default: a new temporary directory)"),
          },
        },
        run: device.run,
      },
      {
        name: "reset",
        short: tr("Reset the session"),
        args: [0, 0],
        run: device.reset,
      },
      {
        name: "api",
        short: tr(
          "Print the TypeScript declarations of the globals and modules",
        ),
        args: [0, 0],
        run: device.api,
      },
      dataCommands("", device.replData),
    ],
  },
  {
    name: "sideload",
    short: tr("Sideload Swipt"),
    options: sideloadOptions,
    commands: [
      {
        name: "install",
        short: tr("Install Swipt"),
        args: [0, 0],
        options: {
          ...passcodeOptions(tr("set up auto-unlock with this passcode")),
          ...nightlyOption,
          ...setupOptions,
        },
        run: sideload.install,
      },
      {
        name: "update",
        short: tr("Update or renew Swipt"),
        args: [0, 0],
        options: {
          ...passcodeOptions(tr("unlock device with this passcode")),
          ...nightlyOption,
          ...setupOptions,
        },
        run: sideload.update,
      },
      {
        name: "setup",
        short: tr("Set up Swipt"),
        args: [0, 0],
        options: {
          ...passcodeOptions(tr("set up auto-unlock with this passcode")),
          ...setupStepOptions,
        },
        run: sideload.setup,
      },
      {
        name: "uninstall",
        short: tr("Remove Swipt"),
        args: [0, 0],
        run: sideload.uninstall,
      },
    ],
  },
  {
    name: "version",
    short: tr("Print the version"),
    args: [0, 0],
    run: device.printVersion,
  },
];

const root: Command = {
  name: "swipt",
  short: tr("Swipt CLI"),
  commands,
};

const parseOptions = (options: Options) =>
  Object.fromEntries(
    Object.entries(options ?? {}).map(([name, option]) => [
      name,
      { type: option.type, ...(option.short ? { short: option.short } : {}) },
    ]),
  );

function allOptions(command: Command, collected = {}) {
  Object.assign(collected, parseOptions(command.options));
  for (const child of command.commands ?? []) {
    allOptions(child, collected);
  }
  return collected;
}

function usage(trail: Command[]) {
  const command = trail.at(-1);
  const name = trail.map((it) => it.name).join(" ");
  const lines = [command.short, ""];
  if (command.commands) {
    lines.push(
      tr("usage: {usage}", { usage: `${name} COMMAND` }),
      "",
      tr("commands:"),
    );
    lines.push(
      ...columns(
        command.commands.map((child) => [
          `  ${child.use ?? child.name}`,
          child.short,
        ]),
      ).split("\n"),
    );
  } else {
    lines.push(
      tr("usage: {usage}", {
        usage: `${trail
          .slice(0, -1)
          .map((it) => it.name)
          .join(" ")} ${command.use ?? command.name}`.replace(/ +/g, " "),
      }),
    );
  }
  if (command.description) {
    lines.push("", ...command.description);
  }
  const options: Options = {
    ...trail.reduce((found, it) => ({ ...found, ...it.options }), {}),
    ...globalOptions,
  };
  lines.push("", tr("options:"));
  lines.push(
    ...columns(
      Object.entries(options).map(([option, it]) => [
        `  ${it.short ? `-${it.short}, ` : ""}--${option}${it.value ? ` ${it.value}` : ""}`,
        it.help,
      ]),
    ).split("\n"),
  );
  return lines.join("\n");
}

function columns(rows) {
  const width = Math.max(...rows.map((row) => row[0].length));
  return rows.map((row) => `${row[0].padEnd(width + 2)}${row[1]}`).join("\n");
}

function resolve(argv) {
  const loose = parseArgs({
    args: argv,
    options: { ...parseOptions(globalOptions), ...allOptions(root) },
    allowPositionals: true,
    strict: false,
    tokens: true,
  });
  const positionals = loose.tokens.filter(
    (token) => token.kind === "positional",
  );
  const trail = [root];
  let consumed = 0;
  let help = positionals[0]?.value === "help";
  if (help) {
    consumed++;
  }
  while (trail.at(-1).commands) {
    const current = trail.at(-1);
    const word = positionals[consumed]?.value;
    if (word === undefined) {
      if (help || loose.values.help || current !== root || argv.length === 0) {
        return { trail, help: true };
      }
      throw new UsageError(tr("a command is missing"));
    }
    const found = current.commands.find((it) => it.name === word);
    if (!found) {
      throw new UsageError(
        tr("unknown command {name} for {parent}", {
          name: JSON.stringify(word),
          parent: JSON.stringify(trail.map((it) => it.name).join(" ")),
        }),
      );
    }
    trail.push(found);
    consumed++;
  }
  const command = trail.at(-1);
  const options: Options = trail.reduce(
    (found, it) => ({ ...found, ...it.options }),
    {},
  );
  const first = positionals[consumed];
  const verbatim =
    first !== undefined && command.passthrough?.(first.value)
      ? first.index + 1
      : argv.length;
  let parsed;
  try {
    parsed = parseArgs({
      args: argv.slice(0, verbatim),
      options: { ...parseOptions(globalOptions), ...parseOptions(options) },
      allowPositionals: true,
      strict: true,
    });
  } catch (error) {
    const option = error.message.match(/^Unknown option '(-[^']*)'/)?.[1];
    throw new UsageError(
      option === undefined
        ? error.message
        : tr("{option} is not an option of {command}", {
            option,
            command: JSON.stringify(trail.map((it) => it.name).join(" ")),
          }),
    );
  }
  if (parsed.values.help) {
    help = true;
  }
  const args = [...parsed.positionals.slice(consumed), ...argv.slice(verbatim)];
  if (
    !help &&
    (args.length < command.args[0] || args.length > command.args[1])
  ) {
    const [least, most] = command.args;
    throw new UsageError(
      least === most
        ? tr("accepts {count} arg(s), received {received}", {
            count: least,
            received: args.length,
          })
        : most === Infinity
          ? tr("accepts at least {count} arg(s), received {received}", {
              count: least,
              received: args.length,
            })
          : tr(
              "accepts between {least} and {count} arg(s), received {received}",
              { least, count: most, received: args.length },
            ),
    );
  }
  return { trail, help, args, values: parsed.values };
}

function printError(error, target, operation?) {
  let exit = 1;
  let message = error.message;
  if (error instanceof UsageError) {
    exit = 2;
  } else if (error instanceof RpcError) {
    switch (error.code) {
      case Code.Unavailable:
        exit = 5;
        message = tr("Swipt at {url} is unreachable: {message}", {
          url: target.url,
          message,
        });
        break;
      case Code.Unauthenticated:
        message = tr(
          "{message}; connect with the URL from Swipt › Developer: swipt device add {url}",
          { message, url: connection.withToken(target.url) },
        );
        break;
      case Code.Unimplemented:
        message = tr(
          "Swipt at {url} does not have this command; update Swipt",
          { url: target.url },
        );
        break;
    }
  }
  console.error(
    tr("error: {message}", {
      message: operation === undefined ? message : `${operation}: ${message}`,
    }),
  );
  if (exit === 2) {
    console.error(tr('run "swipt help" for usage'));
  }
  return exit;
}

async function main() {
  const interrupt = new AbortController();
  const interrupted = () => interrupt.abort();
  process.once("SIGINT", interrupted);
  process.once("SIGTERM", interrupted);
  const target = { url: "" };
  try {
    const resolved = resolve(process.argv.slice(2));
    if (resolved.help) {
      console.log(usage(resolved.trail));
      return;
    }
    const command = resolved.trail.at(-1);
    const values = command.parse
      ? command.parse(resolved.values)
      : resolved.values;
    await command.run({
      args: resolved.args,
      options: values,
      target,
      signal: interrupt.signal,
      printError: (error, operation) => printError(error, target, operation),
    });
  } catch (error) {
    if (error instanceof FalseResult) {
      process.exitCode = 1;
    } else if (interrupt.signal.aborted || error.name === "ExitPromptError") {
      process.exitCode = 130;
    } else {
      process.exitCode = printError(error, target);
    }
  } finally {
    process.off("SIGINT", interrupted);
    process.off("SIGTERM", interrupted);
  }
}

await main();
await Promise.all(
  [process.stdout, process.stderr].map(
    (stream) => new Promise((resolve) => stream.write("", resolve)),
  ),
);
process.exit();
