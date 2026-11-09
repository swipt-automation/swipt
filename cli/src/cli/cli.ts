import { parseArgs } from "node:util";

import { Code, RpcError, UsageError } from "../client";
import * as connection from "../connection";
import { tr } from "../i18n";
import { FalseResult } from "../output";
import type { Command, Options } from "./command";
import { root } from "./commands";

const globalOptions: Options = {
  help: { type: "boolean", short: "h", help: tr("help") },
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

export async function main() {
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
