import type {
  Process,
  ProcessAttributes,
  ResourceUsage,
  Signal,
  SystemAttributes,
} from "@swipt/api/process";

import { invoke, stream } from "./async";
import { act, check, optionsOf, signalOf, type Options } from "./options";

export const list = (options?: Options) =>
  invoke<Process[]>("process.list", [], {
    signal: signalOf(check(options, [])),
  });

// sysmond sampled every 500 ms at the soonest, and at an interval of 750 ms
// gave samples 710 to 1535 ms apart (iPad17,1, iPadOS 27.3 24C5037i).
const sampling = 500;

interface Wire {
  readonly system: readonly unknown[];
  readonly processes: readonly (readonly [
    pid: number,
    executablePath: string,
    bundleId: string,
    ...values: unknown[],
  ])[];
}

const attributesOf = (name: string, value: unknown): readonly string[] => {
  if (value === undefined) {
    return [];
  }
  if (
    !Array.isArray(value) ||
    value.some((attribute) => typeof attribute !== "string")
  ) {
    throw new TypeError(`${name} must be an array of attribute names`);
  }
  return value;
};

export const resourceUsage = <
  ProcessAttribute extends keyof ProcessAttributes = never,
  SystemAttribute extends keyof SystemAttributes = never,
>(
  options?: Options & {
    readonly intervalMs?: number;
    readonly processAttributes?: readonly ProcessAttribute[];
    readonly systemAttributes?: readonly SystemAttribute[];
  },
) => {
  const fields = optionsOf(options, [
    "intervalMs",
    "processAttributes",
    "systemAttributes",
  ]);
  const { intervalMs } = fields;
  if (
    intervalMs !== undefined &&
    (typeof intervalMs !== "number" ||
      intervalMs <= 0 ||
      !Number.isInteger(intervalMs / sampling))
  ) {
    throw new TypeError(
      `intervalMs must be a positive multiple of ${sampling}`,
    );
  }
  const processAttributes = attributesOf(
    "processAttributes",
    fields.processAttributes,
  );
  const systemAttributes = attributesOf(
    "systemAttributes",
    fields.systemAttributes,
  );
  return stream<ResourceUsage<ProcessAttribute, SystemAttribute>>(
    "process.resourceUsage",
    [{ intervalMs, processAttributes, systemAttributes }],
    signalOf(options),
    "latest",
    (value) => {
      const { system, processes, ...usage } = value as Wire;
      return {
        ...usage,
        system: Object.fromEntries(
          systemAttributes.map((name, index) => [name, system[index]]),
        ),
        processes: processes.map(
          ([pid, executablePath, bundleId, ...values]) => ({
            ...Object.fromEntries(
              processAttributes.flatMap((name, index) =>
                values[index] === null ? [] : [[name, values[index]]],
              ),
            ),
            pid,
            ...(executablePath === "" ? undefined : { executablePath }),
            ...(bundleId === "" ? undefined : { bundleId }),
          }),
        ),
      } as unknown as ResourceUsage<ProcessAttribute, SystemAttribute>;
    },
  );
};

const signals: readonly Signal[] = [
  "SIGHUP",
  "SIGINT",
  "SIGQUIT",
  "SIGILL",
  "SIGTRAP",
  "SIGABRT",
  "SIGEMT",
  "SIGFPE",
  "SIGKILL",
  "SIGBUS",
  "SIGSEGV",
  "SIGSYS",
  "SIGPIPE",
  "SIGALRM",
  "SIGTERM",
  "SIGURG",
  "SIGSTOP",
  "SIGTSTP",
  "SIGCONT",
  "SIGCHLD",
  "SIGTTIN",
  "SIGTTOU",
  "SIGIO",
  "SIGXCPU",
  "SIGXFSZ",
  "SIGVTALRM",
  "SIGPROF",
  "SIGWINCH",
  "SIGINFO",
  "SIGUSR1",
  "SIGUSR2",
];

export const kill = (
  pid: number,
  signal: Signal | number = "SIGTERM",
  options?: Options,
) => {
  if (!Number.isSafeInteger(pid) || pid < 0) {
    throw new TypeError("the process ID must be a non-negative integer");
  }
  const number =
    typeof signal === "number" ? signal : signals.indexOf(signal) + 1;
  if (!Number.isInteger(number) || number < 1 || number > signals.length) {
    throw new TypeError(
      `the signal must be a signal name or an integer from 1 to ${signals.length}`,
    );
  }
  return act("process.kill", [pid, number], check(options, []));
};
