import { timestampFromMs } from "@bufbuild/protobuf/wkt";

import { UsageError } from "../../../client";
import { tr } from "../../../i18n";
import { printLog } from "../../../output";
import { defineCommand } from "../../command";
import { session } from "../../session";
import { deviceOption } from "../flags";

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

const readLogs = (context) =>
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

export const logs = defineCommand({
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
  run: readLogs,
});
