/** @available apiLevel 4, android */
import type { CallOptions } from "../index";

export type Signal =
  | "SIGHUP"
  | "SIGINT"
  | "SIGQUIT"
  | "SIGILL"
  | "SIGTRAP"
  | "SIGABRT"
  | "SIGBUS"
  | "SIGFPE"
  | "SIGKILL"
  | "SIGUSR1"
  | "SIGSEGV"
  | "SIGUSR2"
  | "SIGPIPE"
  | "SIGALRM"
  | "SIGTERM"
  | "SIGSTKFLT"
  | "SIGCHLD"
  | "SIGCONT"
  | "SIGSTOP"
  | "SIGTSTP"
  | "SIGTTIN"
  | "SIGTTOU"
  | "SIGURG"
  | "SIGXCPU"
  | "SIGXFSZ"
  | "SIGVTALRM"
  | "SIGPROF"
  | "SIGWINCH"
  | "SIGIO"
  | "SIGPWR"
  | "SIGSYS";

export interface ExecResult {
  readonly exitCode: number;
  readonly stdout: string;
  readonly stderr: string;
}

export interface SpawnOptions extends CallOptions {
  readonly directory?: string;
  readonly environment?: Readonly<Record<string, string>>;
}

export interface ExecOptions extends SpawnOptions {
  readonly stdin?: string | Uint8Array;
}

export interface ShellProcess {
  readonly pid: number;
  readonly stdin: WritableStream<ArrayBuffer | ArrayBufferView>;
  readonly stdout: ReadableStream<Uint8Array>;
  readonly stderr: ReadableStream<Uint8Array>;
  /** Answers the exit code, which is 128 plus the number of the signal for a process that a signal ended. */
  wait(options?: CallOptions): Promise<number>;
  /** Signals the process and the processes it started. */
  kill(signal?: Signal | number, options?: CallOptions): Promise<void>;
}

/** A string runs through /system/bin/sh -c. */
export declare function exec(
  command: string | readonly string[],
  options?: ExecOptions,
): Promise<ExecResult>;
/** A string runs through /system/bin/sh -c. Aborting the signal kills the process. */
export declare function spawn(
  command: string | readonly string[],
  options?: SpawnOptions,
): Promise<ShellProcess>;
