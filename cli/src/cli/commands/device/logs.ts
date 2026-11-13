import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";

import { UsageError } from "../../../client";
import { t } from "../../../i18n";
import { printLog } from "../../../output";
import { LogEntrySchema, LogLevel } from "../../../proto/common_pb";
import { type Context, defineCommand } from "../../command";
import { session } from "../../session";
import { deviceOption } from "../flags";

const durationUnits = new Map([
  ["ns", 1e-6],
  ["us", 1e-3],
  ["µs", 1e-3],
  ["ms", 1],
  ["s", 1000],
  ["m", 60_000],
  ["h", 3_600_000],
]);

const levels = new Map([
  ["error", LogLevel.ERROR],
  ["warn", LogLevel.WARN],
  ["info", LogLevel.INFO],
  ["debug", LogLevel.DEBUG],
]);

function parseDuration(text: string | undefined, option: string) {
  if (text === undefined) {
    return 0;
  }
  if (/^-?\d+$/.test(text) && Number(text) === 0) {
    return 0;
  }
  let total = 0;
  let rest = text;
  while (rest !== "") {
    const match = /^(\d+(?:\.\d*)?|\.\d+)([^\d.]+)/.exec(rest);
    const unit = match === null ? undefined : durationUnits.get(match[2]);
    if (match === null || unit === undefined) {
      throw new UsageError(
        t("commands.device.logs.invalidDuration", {
          value: JSON.stringify(text),
          option,
        }),
      );
    }
    total += Number(match[1]) * unit;
    rest = rest.slice(match[0].length);
  }
  return total;
}

function parseCount(text: string | undefined, option: string) {
  if (text === undefined) {
    return undefined;
  }
  if (!/^\d+$/.test(text) || Number(text) > 0xffffffff) {
    throw new UsageError(
      t("commands.device.logs.invalidNumber", {
        value: JSON.stringify(text),
        option,
      }),
    );
  }
  return Number(text);
}

async function readLogs(
  context: Context<{
    device?: string;
    follow?: boolean;
    tail?: string;
    since?: string;
    level?: string;
    swiplet?: string;
  }>,
) {
  const { options } = context;
  const tail = parseCount(options.tail, "--tail");
  const since = parseDuration(options.since, "--since");
  const level = levels.get(options.level ?? "info");
  if (level === undefined) {
    throw new UsageError(t("commands.device.logs.invalidLevel"));
  }
  await session(context, async (client) => {
    for await (const response of client.logs.readLogs(
      {
        follow: options.follow,
        tail: tail ?? (since > 0 ? 0xffffffff : 100),
        swipletId: options.swiplet ?? "",
        level,
        since: since > 0 ? timestampFromMs(Date.now() - since) : undefined,
      },
      { signal: context.signal },
    )) {
      printLog(response.entry ?? create(LogEntrySchema), true);
    }
  });
}

export const logs = defineCommand({
  name: "logs",
  short: t("commands.device.logs.description"),
  args: [0, 0],
  options: {
    ...deviceOption,
    follow: { type: "boolean", help: t("commands.device.logs.follow") },
    tail: {
      type: "string",
      value: "COUNT",
      help: t("commands.device.logs.count"),
    },
    since: {
      type: "string",
      value: "DURATION",
      help: t("commands.device.logs.since"),
    },
    level: {
      type: "string",
      value: "LEVEL",
      help: t("commands.device.logs.level"),
    },
    swiplet: {
      type: "string",
      value: "ID",
      help: t("commands.device.logs.swiplet"),
    },
  },
  run: readLogs,
});
