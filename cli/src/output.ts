import { styleText } from "node:util";

import { type Timestamp, timestampDate } from "@bufbuild/protobuf/wkt";
import {
  type Status,
  createPrompt,
  isBackspaceKey,
  isDownKey,
  isEnterKey,
  isNumberKey,
  isUpKey,
  makeTheme,
  useEffect,
  useKeypress,
  usePagination,
  usePrefix,
  useRef,
  useState,
} from "@inquirer/core";
import select from "@inquirer/select";
import stringWidth from "string-width";

import { t } from "./i18n";
import {
  type LogEntry,
  LogLevel,
  LogLevelSchema,
  type ScriptError,
  type StackFrame,
} from "./proto/common_pb";
import { type ServiceState, ServiceStatus } from "./proto/swiplets_pb";

export class FalseResult extends Error {}

export const errorMessage = (error: unknown) =>
  error instanceof Error ? error.message : String(error);

export const selectTheme = {
  style: {
    keysHelpTip: () =>
      [
        [`↑↓`, t("output.navigate")],
        [`⏎`, t("output.select")],
      ]
        .map(
          ([key, action]) =>
            `${styleText("bold", key)} ${styleText("dim", action)}`,
        )
        .join(styleText("dim", " • ")),
  },
};

export interface PromptContext {
  options: { device?: string; team?: string };
  signal: AbortSignal;
}

export const interactive = (context: Pick<PromptContext, "options">) =>
  context.options.device === undefined &&
  context.options.team === undefined &&
  process.stdin.isTTY &&
  process.stderr.isTTY;

export interface Choice<Value> {
  key?: string;
  name: string;
  short?: string;
  value: Value;
}

export interface ChoiceStream<Value> {
  current: () => {
    choices: Choice<Value>[];
    loading?: string;
    autoSelect?: boolean;
  };
  subscribe: (listener: () => void) => () => void;
}

export function choose<Value>(
  context: PromptContext,
  message: string,
  choices: { name: string; short?: string; value: Value }[],
  failure: Error,
) {
  if (!interactive(context)) {
    throw failure;
  }
  return select(
    { message, choices, theme: selectTheme },
    { output: process.stderr, signal: context.signal },
  );
}

const spinner = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏";

const streamPrompt = createPrompt<
  unknown,
  { message: string; stream: ChoiceStream<unknown> }
>((config, done) => {
  const theme = makeTheme(selectTheme);
  const { keybindings } = theme;
  const [status, setStatus] = useState<Status>("idle");
  const prefix = usePrefix({ status, theme });
  const [snapshot, setSnapshot] = useState(() => config.stream.current());
  const [frame, setFrame] = useState(0);
  const keyOf = (choice: Choice<unknown>) => choice.key ?? choice.name;
  const [activeKey, setActiveKey] = useState<string | undefined>(() =>
    snapshot.choices[0] ? keyOf(snapshot.choices[0]) : undefined,
  );
  const interacted = useRef(false);
  const settled = useRef<Choice<unknown> | undefined>(undefined);
  const searchTimeout = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );

  useEffect(() => {
    const sync = () => {
      const next = config.stream.current();
      setSnapshot(next);
      if (
        next.autoSelect &&
        next.loading === undefined &&
        next.choices.length === 1 &&
        !interacted.current &&
        settled.current === undefined
      ) {
        settled.current = next.choices[0];
        setStatus("done");
        done(next.choices[0].value);
      }
    };
    const unsubscribe = config.stream.subscribe(sync);
    sync();
    return unsubscribe;
  }, [config.stream]);

  const isLoading = snapshot.loading !== undefined;
  useEffect(() => {
    if (!isLoading) {
      return;
    }
    const timer = setInterval(() => {
      setFrame((current) => (current + 1) % spinner.length);
    }, 80);
    return () => clearInterval(timer);
  }, [isLoading]);

  useEffect(() => () => clearTimeout(searchTimeout.current), []);

  const items = snapshot.choices;
  const foundIndex =
    activeKey === undefined
      ? -1
      : items.findIndex((item) => keyOf(item) === activeKey);
  const active = foundIndex === -1 ? 0 : foundIndex;
  const selectedChoice = items[active];

  useKeypress((key, rl) => {
    clearTimeout(searchTimeout.current);
    interacted.current = true;
    if (isEnterKey(key)) {
      if (selectedChoice !== undefined && settled.current === undefined) {
        settled.current = selectedChoice;
        setStatus("done");
        done(selectedChoice.value);
      }
    } else if (isUpKey(key, keybindings) || isDownKey(key, keybindings)) {
      rl.clearLine(0);
      if (items.length > 1) {
        const offset = isUpKey(key, keybindings) ? -1 : 1;
        const next = (active + offset + items.length) % items.length;
        setActiveKey(keyOf(items[next]));
      }
    } else if (isNumberKey(key) && !Number.isNaN(Number(rl.line))) {
      const item = items[Number(rl.line) - 1];
      if (item !== undefined) {
        setActiveKey(keyOf(item));
      }
      searchTimeout.current = setTimeout(() => rl.clearLine(0), 700);
    } else if (isBackspaceKey(key)) {
      rl.clearLine(0);
    } else {
      const term = rl.line.toLowerCase();
      const match = items.find((item) =>
        item.name.toLowerCase().startsWith(term),
      );
      if (match !== undefined) {
        setActiveKey(keyOf(match));
      }
      searchTimeout.current = setTimeout(() => rl.clearLine(0), 700);
    }
  });

  const message = theme.style.message(config.message, status);
  if (status === "done") {
    const chosen = settled.current ?? selectedChoice;
    return [
      prefix,
      message,
      chosen ? theme.style.answer(chosen.short ?? chosen.name) : "",
    ]
      .filter(Boolean)
      .join(" ");
  }

  const page =
    items.length === 0
      ? ""
      : usePagination({
          items,
          active,
          renderItem({ item, isActive }) {
            const line = `${isActive ? "❯" : " "} ${item.name}`;
            return isActive ? theme.style.highlight(line) : line;
          },
          pageSize: 7,
          loop: true,
        });

  const stream = process.stderr;
  const loadingLine =
    snapshot.loading === undefined
      ? ""
      : `  ${styleText("cyan", spinner[frame], { stream })} ${styleText("dim", snapshot.loading, { stream })}`;

  const lines = [
    [prefix, message].filter(Boolean).join(" "),
    page,
    loadingLine,
    " ",
    selectTheme.style.keysHelpTip(),
  ]
    .filter(Boolean)
    .join("\n")
    .trimEnd();
  return `${lines}\x1b[?25l`;
});

export function selectStream<Value>(
  context: Pick<PromptContext, "signal">,
  message: string,
  stream: ChoiceStream<Value>,
): Promise<Value> {
  return streamPrompt(
    { message, stream: stream as ChoiceStream<unknown> },
    { output: process.stderr, signal: context.signal },
  ) as Promise<Value>;
}

export async function step<Result>(
  label: string,
  run: (detail: (text: string) => void) => Result | Promise<Result>,
  { transient = false } = {},
) {
  const stream = process.stderr;
  if (!stream.isTTY) {
    console.error(label);
    return run(() => {});
  }
  const style = (format: Parameters<typeof styleText>[0], text: string) =>
    styleText(format, text, { stream });
  const started = Date.now();
  const elapsed = () => {
    const seconds = Math.floor((Date.now() - started) / 1000);
    return seconds > 0 ? style("dim", ` ${seconds}s`) : "";
  };
  const draw = (line: string) =>
    stream.write(`\r\x1b[?7l\x1b[2K${line}\x1b[?7h`);
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

const levelName = (level: LogLevel) =>
  level === LogLevel.UNSPECIFIED
    ? "info"
    : LogLevelSchema.value[level].name.slice("LOG_LEVEL_".length).toLowerCase();

const pad = (value: number, width: number) =>
  String(value).padStart(width, "0");

const dateOf = (timestamp: Timestamp | undefined) =>
  timestamp ? timestampDate(timestamp) : new Date(0);

export function clockTime(timestamp: Timestamp | undefined) {
  const at = dateOf(timestamp);
  return `${pad(at.getHours(), 2)}:${pad(at.getMinutes(), 2)}:${pad(at.getSeconds(), 2)}.${pad(at.getMilliseconds(), 3)}`;
}

export function dateTime(timestamp: Timestamp | undefined) {
  const at = dateOf(timestamp);
  return `${at.getFullYear()}-${pad(at.getMonth() + 1, 2)}-${pad(at.getDate(), 2)} ${pad(at.getHours(), 2)}:${pad(at.getMinutes(), 2)}:${pad(at.getSeconds(), 2)}`;
}

export function columns(rows: string[][]) {
  const widths: number[] = [];
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

const frameText = (frame: StackFrame) => {
  const position = `${frame.file}:${frame.line}:${frame.column}`;
  return frame.function === "" ? position : `${frame.function} (${position})`;
};

export const failureText = (failure: ScriptError) =>
  [
    `${failure.name}: ${failure.message}`,
    ...failure.stack.map((frame) => `    at ${frameText(frame)}`),
  ].join("\n");

export function printScriptError(failure: ScriptError) {
  console.error(t("error", { message: failureText(failure) }));
  return new FalseResult();
}

export function printLog(entry: LogEntry, toStdout: boolean) {
  const message =
    entry.tag === "" ? entry.message : `${entry.tag}: ${entry.message}`;
  (toStdout ? console.log : console.error)(
    clockTime(entry.time),
    levelName(entry.level).toUpperCase(),
    message,
  );
}

const serviceStatuses = {
  [ServiceStatus.UNSPECIFIED]: t("output.serviceStatus.stopped"),
  [ServiceStatus.STOPPED]: t("output.serviceStatus.stopped"),
  [ServiceStatus.STARTING]: t("output.serviceStatus.starting"),
  [ServiceStatus.RUNNING]: t("output.serviceStatus.running"),
  [ServiceStatus.FAILED]: t("output.serviceStatus.failed"),
};

export const serviceEnabledText = (enabled: boolean | undefined) =>
  enabled
    ? t("output.serviceStatus.turnedOn")
    : t("output.serviceStatus.turnedOff");

export const serviceStatusText = (status: ServiceStatus) =>
  serviceStatuses[status];

export function serviceText(state: ServiceState | undefined) {
  const enabled = serviceEnabledText(state?.enabled);
  const status = serviceStatuses[state?.status ?? ServiceStatus.UNSPECIFIED];
  let text = state?.since
    ? t("output.serviceSince", {
        enabled,
        status,
        time: clockTime(state.since),
      })
    : t("output.service", { enabled, status });
  if (state?.error) {
    text += `: ${failureText(state.error)}`;
  }
  return text;
}
