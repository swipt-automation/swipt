import type {
  ExecResult,
  ShellProcess as ShellProcessInterface,
  Signal,
} from "@swipt/api/android/shell";

import { invoke } from "../async";
import { failureOf } from "../errors";
import { onAbort } from "../events";
import { native } from "../native";
import { check, signalOf, type Options } from "../options";
import { nativeReadable, nativeWritable } from "../streams";
import { bytes, callData, paramsOf } from "./call";

interface Spawned {
  readonly process: string;
  readonly pid: number;
  readonly stdin: string;
  readonly stdout: string;
  readonly stderr: string;
}

interface State {
  readonly id: string;
  exitCode?: number;
  aborted?: { readonly reason: unknown };
  release(): void;
}

const signals: readonly Signal[] = [
  "SIGHUP",
  "SIGINT",
  "SIGQUIT",
  "SIGILL",
  "SIGTRAP",
  "SIGABRT",
  "SIGBUS",
  "SIGFPE",
  "SIGKILL",
  "SIGUSR1",
  "SIGSEGV",
  "SIGUSR2",
  "SIGPIPE",
  "SIGALRM",
  "SIGTERM",
  "SIGSTKFLT",
  "SIGCHLD",
  "SIGCONT",
  "SIGSTOP",
  "SIGTSTP",
  "SIGTTIN",
  "SIGTTOU",
  "SIGURG",
  "SIGXCPU",
  "SIGXFSZ",
  "SIGVTALRM",
  "SIGPROF",
  "SIGWINCH",
  "SIGIO",
  "SIGPWR",
  "SIGSYS",
];
const lastSignal = 64;

const signalNumber = (signal: unknown): number => {
  const number =
    typeof signal === "number" ? signal : signals.indexOf(signal as Signal) + 1;
  if (!Number.isInteger(number) || number < 1 || number > lastSignal) {
    throw new TypeError(
      `the signal must be a signal name or an integer from 1 to ${lastSignal}`,
    );
  }
  return number;
};

const commandOf = (command: unknown): Record<string, unknown> => {
  if (typeof command === "string") {
    return { command };
  }
  if (
    Array.isArray(command) &&
    command.length > 0 &&
    command.every((argument) => typeof argument === "string")
  ) {
    return { arguments: command };
  }
  throw new TypeError(
    "the command must be a string or a non-empty array of strings",
  );
};

export const exec = async (
  command: unknown,
  options?: unknown,
): Promise<ExecResult> => {
  const { stdin, ...params } = paramsOf(options, [
    "stdin",
    "directory",
    "environment",
  ]);
  const { value, data } = await callData<{ exitCode: number; stderr: string }>(
    "shell.exec",
    { ...params, ...commandOf(command) },
    options as never,
    stdin === undefined ? undefined : bytes(stdin, "stdin"),
  );
  return Object.freeze({
    exitCode: value.exitCode,
    stdout: new TextDecoder().decode(data),
    stderr: value.stderr,
  });
};

const states = new WeakMap<ShellProcess, State>();

class ShellProcess {
  declare readonly pid: number;
  declare readonly stdin: ShellProcessInterface["stdin"];
  declare readonly stdout: ShellProcessInterface["stdout"];
  declare readonly stderr: ShellProcessInterface["stderr"];

  constructor(spawned: Spawned, signal: AbortSignal | undefined) {
    const state: State = { id: spawned.process, release: () => {} };
    const abort = () => {
      state.aborted = { reason: signal!.reason };
      native.release(spawned.process, failureOf(signal!.reason));
    };
    if (signal?.aborted) {
      abort();
    } else if (signal !== undefined) {
      state.release = onAbort(signal, abort);
    }
    states.set(this, state);
    Object.defineProperties(this, {
      pid: { value: spawned.pid, enumerable: true },
      stdin: { value: nativeWritable(spawned.stdin), enumerable: true },
      stdout: { value: nativeReadable(spawned.stdout), enumerable: true },
      stderr: { value: nativeReadable(spawned.stderr), enumerable: true },
    });
    Object.freeze(this);
  }

  async wait(options?: Options): Promise<number> {
    const state = states.get(this)!;
    const signal = signalOf(check(options, []));
    if (state.exitCode !== undefined) {
      return state.exitCode;
    }
    if (state.aborted !== undefined) {
      throw state.aborted.reason;
    }
    const exitCode = await invoke<number>("shell.wait", [state.id], {
      signal,
    });
    if (state.exitCode === undefined) {
      state.exitCode = exitCode;
      state.release();
      native.release(state.id);
    }
    return exitCode;
  }

  async kill(signal: unknown = "SIGTERM", options?: Options): Promise<void> {
    const number = signalNumber(signal);
    const callSignal = signalOf(check(options, []));
    const state = states.get(this)!;
    if (state.aborted !== undefined) {
      return;
    }
    if (state.exitCode !== undefined) {
      await invoke("shell.killExited", [this.pid, number], {
        signal: callSignal,
      });
      return;
    }
    await invoke("shell.kill", [state.id, number], { signal: callSignal });
  }

  toString(): string {
    return `ShellProcess ${this.pid}`;
  }
}

export const spawn = async (
  command: unknown,
  options?: unknown,
): Promise<ShellProcess> => {
  const params = paramsOf(options, ["directory", "environment"]);
  const signal = signalOf(options);
  const spawned = await invoke<Spawned>(
    "shell.spawn",
    [{ ...params, ...commandOf(command) }],
    { signal },
  );
  return new ShellProcess(spawned, signal);
};
