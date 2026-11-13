import { parseArgs } from "node:util";

import { Code, RpcError, UsageError } from "../client";
import * as connection from "../connection";
import { tr } from "../i18n";
import { FalseResult, errorMessage } from "../output";
import type { Command, Options } from "./command";
import { root } from "./commands";

const globalOptions: Options = {
  help: { type: "boolean", short: "h", help: tr("help") },
};

const parseOptions = (options: Options = {}) =>
  Object.fromEntries(
    Object.entries(options).map(([name, option]) => [
      name,
      { type: option.type, ...(option.short ? { short: option.short } : {}) },
    ]),
  );

function allOptions(command: Command): ReturnType<typeof parseOptions> {
  return Object.assign(
    parseOptions(command.options),
    ...("commands" in command ? command.commands.map(allOptions) : []),
  );
}

function usage(trail: Command[]) {
  const command = trail[trail.length - 1];
  const name = trail.map((it) => it.name).join(" ");
  const lines = [command.short, ""];
  if ("commands" in command) {
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

function columns(rows: [string, string][]) {
  const width = Math.max(...rows.map((row) => row[0].length));
  return rows.map((row) => `${row[0].padEnd(width + 2)}${row[1]}`).join("\n");
}

function resolve(argv: string[]) {
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
  const trail: Command[] = [root];
  let current: Command = root;
  let consumed = 0;
  let help = positionals[0]?.value === "help";
  if (help) {
    consumed++;
  }
  while ("commands" in current) {
    const word = positionals[consumed]?.value;
    if (word === undefined) {
      if (help || loose.values.help || current !== root || argv.length === 0) {
        return { trail };
      }
      throw new UsageError(tr("A command is missing."));
    }
    const found: Command | undefined = current.commands.find(
      (it) => it.name === word,
    );
    if (!found) {
      throw new UsageError(
        tr("unknown command {name} for {parent}", {
          name: JSON.stringify(word),
          parent: JSON.stringify(trail.map((it) => it.name).join(" ")),
        }),
      );
    }
    trail.push(found);
    current = found;
    consumed++;
  }
  const command = current;
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
    const message = errorMessage(error);
    const option = message.match(/^Unknown option '(-[^']*)'/)?.[1];
    throw new UsageError(
      option === undefined
        ? message
        : tr("{option} isn't an option of {command}.", {
            option,
            command: JSON.stringify(trail.map((it) => it.name).join(" ")),
          }),
    );
  }
  if (help || parsed.values.help) {
    return { trail };
  }
  const args = [...parsed.positionals.slice(consumed), ...argv.slice(verbatim)];
  const [least, most] = command.args;
  if (args.length < least || args.length > most) {
    throw new UsageError(
      least === most
        ? tr(
            {
              one: "accepts {count} arg, received {received}",
              other: "accepts {count} args, received {received}",
            },
            {
              count: least,
              received: args.length,
            },
          )
        : most === Infinity
          ? tr(
              {
                one: "accepts at least {count} arg, received {received}",
                other: "accepts at least {count} args, received {received}",
              },
              {
                count: least,
                received: args.length,
              },
            )
          : tr(
              {
                one: "accepts between {least} and {count} arg, received {received}",
                other:
                  "accepts between {least} and {count} args, received {received}",
              },
              { least, count: most, received: args.length },
            ),
    );
  }
  return { trail, invocation: { command, args, options: parsed.values } };
}

function printError(
  error: unknown,
  target: { url: string },
  operation?: string,
) {
  let exit = 1;
  let message = errorMessage(error);
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
          '{message}; connect with the URL that Swipt › Developer or "swipt driver" shows: swipt device add {url}',
          {
            message,
            url: connection.withToken(target.url),
          },
        );
        break;
      case Code.Unimplemented:
        message = tr(
          "Swipt at {url} doesn't have this command; update Swipt.",
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
    console.error(tr('Run "swipt help" for usage.'));
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
    const { trail, invocation } = resolve(process.argv.slice(2));
    if (!invocation) {
      console.log(usage(trail));
      return;
    }
    await invocation.command.run({
      args: invocation.args,
      options: invocation.options,
      target,
      signal: interrupt.signal,
      printError: (error, operation) => printError(error, target, operation),
    });
  } catch (error) {
    if (error instanceof FalseResult) {
      process.exitCode = 1;
    } else if (
      interrupt.signal.aborted ||
      (error instanceof Error && error.name === "ExitPromptError")
    ) {
      process.exitCode = 130;
    } else {
      process.exitCode = printError(error, target);
    }
  } finally {
    process.off("SIGINT", interrupted);
    process.off("SIGTERM", interrupted);
  }
}
