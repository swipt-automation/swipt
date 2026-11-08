import { styleText } from "node:util";

import { timestampDate } from "@bufbuild/protobuf/wkt";
import select from "@inquirer/select";
import stringWidth from "string-width";

import { tr } from "./i18n";
import { LogLevel, LogLevelSchema } from "./proto/common_pb";
import { ServiceStatus } from "./proto/swiplets_pb";

export class FalseResult extends Error {}

export const selectTheme = {
  style: {
    keysHelpTip: () =>
      [
        [`↑↓`, tr("navigate")],
        [`⏎`, tr("select")],
      ]
        .map(
          ([key, action]) =>
            `${styleText("bold", key)} ${styleText("dim", action)}`,
        )
        .join(styleText("dim", " • ")),
  },
};

export const interactive = (context) =>
  context.options.device === undefined &&
  context.options.team === undefined &&
  process.stdin.isTTY &&
  process.stderr.isTTY;

export function choose(context, message, choices, failure) {
  if (!interactive(context)) {
    throw failure;
  }
  return select(
    { message, choices, theme: selectTheme },
    { output: process.stderr, signal: context.signal },
  );
}

const spinner = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏";

export async function step(label, run, { transient = false } = {}) {
  const stream = process.stderr;
  if (!stream.isTTY) {
    console.error(label);
    return run(() => {});
  }
  const style = (format, text) => styleText(format, text, { stream });
  const started = Date.now();
  const elapsed = () => {
    const seconds = Math.floor((Date.now() - started) / 1000);
    return seconds > 0 ? style("dim", ` ${seconds}s`) : "";
  };
  const draw = (line) => stream.write(`\r\x1b[?7l\x1b[2K${line}\x1b[?7h`);
  let detail = "";
  let frame = 0;
  const render = () => {
    frame = (frame + 1) % spinner.length;
    draw(
      `${style("cyan", spinner[frame])} ${label}${detail && style("dim", `  ${detail}`)}${elapsed()}`,
    );
  };
  stream.write("\x1b[?25l");
  render();
  const timer = setInterval(render, 80);
  try {
    const result = await run((text) => {
      detail = text;
    });
    clearInterval(timer);
    draw(transient ? "" : `${style("green", "✔")} ${label}${elapsed()}\n`);
    return result;
  } catch (error) {
    clearInterval(timer);
    draw(`${style("red", "✖")} ${label}${elapsed()}\n`);
    throw error;
  } finally {
    stream.write("\x1b[?25h");
  }
}

const levelName = (level) =>
  level === LogLevel.UNSPECIFIED
    ? "info"
    : LogLevelSchema.value[level].name.slice("LOG_LEVEL_".length).toLowerCase();

const pad = (value, width) => String(value).padStart(width, "0");

export function clockTime(timestamp) {
  const at = timestampDate(timestamp);
  return `${pad(at.getHours(), 2)}:${pad(at.getMinutes(), 2)}:${pad(at.getSeconds(), 2)}.${pad(at.getMilliseconds(), 3)}`;
}

export function dateTime(timestamp) {
  const at = timestampDate(timestamp);
  return `${at.getFullYear()}-${pad(at.getMonth() + 1, 2)}-${pad(at.getDate(), 2)} ${pad(at.getHours(), 2)}:${pad(at.getMinutes(), 2)}:${pad(at.getSeconds(), 2)}`;
}

export function columns(rows) {
  const widths = [];
  for (const row of rows) {
    row.forEach((cell, index) => {
      widths[index] = Math.max(widths[index] ?? 0, stringWidth(cell));
    });
  }
  return rows
    .map((row) =>
      row
        .map((cell, index) =>
          index === row.length - 1
            ? cell
            : cell + " ".repeat(widths[index] + 2 - stringWidth(cell)),
        )
        .join("")
        .trimEnd(),
    )
    .join("\n");
}

const frameText = (frame) => {
  const position = `${frame.file}:${frame.line}:${frame.column}`;
  return frame.function === "" ? position : `${frame.function} (${position})`;
};

export const failureText = (failure) =>
  [
    `${failure.name}: ${failure.message}`,
    ...failure.stack.map((frame) => `    at ${frameText(frame)}`),
  ].join("\n");

export function printScriptError(failure) {
  console.error(tr("error: {message}", { message: failureText(failure) }));
  return new FalseResult();
}

export function printLog(entry, toStdout) {
  const message =
    entry.tag === "" ? entry.message : `${entry.tag}: ${entry.message}`;
  (toStdout ? console.log : console.error)(
    clockTime(entry.time),
    levelName(entry.level).toUpperCase(),
    message,
  );
}

const serviceStatuses = {
  [ServiceStatus.UNSPECIFIED]: tr("stopped"),
  [ServiceStatus.STOPPED]: tr("stopped"),
  [ServiceStatus.STARTING]: tr("starting"),
  [ServiceStatus.RUNNING]: tr("running"),
  [ServiceStatus.FAILED]: tr("failed"),
};

export const serviceEnabledText = (enabled) =>
  enabled ? tr("enabled") : tr("disabled");

export const serviceStatusText = (status) => serviceStatuses[status];

export function serviceText(_id, state) {
  const enabled = serviceEnabledText(state?.enabled);
  const status = serviceStatuses[state?.status ?? ServiceStatus.UNSPECIFIED];
  let text = state?.since
    ? tr("{enabled}, {status} since {time}", {
        enabled,
        status,
        time: clockTime(state.since),
      })
    : tr("{enabled}, {status}", { enabled, status });
  if (state?.error) {
    text += `: ${failureText(state.error)}`;
  }
  return text;
}
