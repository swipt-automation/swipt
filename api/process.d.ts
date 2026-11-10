/** @available apiLevel 3 */
import type { CallOptions } from "./index";

export interface Process {
  readonly pid: number;
  readonly executablePath?: string;
  readonly bundleId?: string;
}

export interface ProcessAttributes {
  readonly procFlags: number;
  readonly procStatus: number;
  readonly procXstatus: number;
  readonly pid: number;
  readonly ppid: number;
  readonly uid: number;
  readonly gid: number;
  readonly ruid: number;
  readonly rgid: number;
  readonly svuid: number;
  readonly svgid: number;
  readonly comm: string;
  readonly name: string;
  readonly nfiles: number;
  readonly pgid: number;
  readonly pjobc: number;
  readonly tdev: number;
  readonly tpgid: number;
  readonly nice: number;
  readonly memVirtualSize: number;
  readonly memResidentSize: number;
  readonly cpuTotalUser: number;
  readonly cpuTotalSystem: number;
  readonly threadsUser: number;
  readonly threadsSystem: number;
  readonly policy: number;
  readonly faults: number;
  readonly vmPageIns: number;
  readonly cowFaults: number;
  readonly msgSent: number;
  readonly msgRecv: number;
  readonly sysCallsMach: number;
  readonly sysCallsUnix: number;
  readonly ctxSwitch: number;
  readonly threadCount: number;
  readonly numRunning: number;
  readonly priority: number;
  readonly wqNumThreads: number;
  readonly wqRunThreads: number;
  readonly wqBlockedThreads: number;
  readonly wqState: number;
  readonly intWakeups: number;
  readonly platIdleWakeups: number;
  readonly timerWakeBin1: number;
  readonly timerWakeBin2: number;
  readonly machPortCount: number;
  /** Absent without a usable CPU-time baseline. */
  readonly cpuUsage?: number;
  /** Absent without a usable CPU-time baseline. */
  readonly powerScore?: number;
  readonly memRPrvt: number;
  readonly memRShrd: number;
  readonly responsiblePID: number;
  readonly appSleep: boolean;
  readonly latencyQosTier: number;
  readonly throughputQosTier: number;
  readonly memPurgeable: number;
  readonly totalEnergyScore: number;
  readonly procAge: number;
  readonly avgPowerScore: number;
  readonly startAbsTime: number;
  readonly wiredSize: number;
  readonly physFootprint: number;
  readonly memAnon: number;
  readonly memAnonPeak: number;
  readonly uniqueID: number;
  readonly parentUniqueID: number;
  readonly responsibleUniqueID: number;
  readonly diskBytesRead: number;
  readonly diskBytesWritten: number;
  readonly memCompressed: number;
  readonly coalitionID: number;
  readonly __arch: number;
  readonly __restricted: boolean;
  readonly __sandbox: boolean;
  readonly __suddenTerm: boolean;
}

export interface SystemAttributes {
  readonly vmFreeCount: number;
  readonly vmActiveCount: number;
  readonly vmInactiveCount: number;
  readonly vmWireCount: number;
  readonly vmZeroFillCount: number;
  readonly vmReactivations: number;
  readonly vmPageIns: number;
  readonly vmPageOuts: number;
  readonly vmFaults: number;
  readonly vmCowFaults: number;
  readonly vmLookups: number;
  readonly vmHits: number;
  readonly vmPurges: number;
  readonly vmPurgeableCount: number;
  readonly vmSpeculativeCount: number;
  readonly vmDecompressions: number;
  readonly vmCompressions: number;
  readonly vmSwapIns: number;
  readonly vmSwapOuts: number;
  readonly vmCompressorPageCount: number;
  readonly vmThrottledCount: number;
  readonly threadCount: number;
  readonly diskReadOps: number;
  readonly diskWriteOps: number;
  readonly diskBytesRead: number;
  readonly diskBytesWritten: number;
  readonly netPacketsIn: number;
  readonly netBytesIn: number;
  readonly netPacketsOut: number;
  readonly netBytesOut: number;
  readonly vmExtPageCount: number;
  readonly vmIntPageCount: number;
  readonly vmTotalUncompPagesInComp: number;
  readonly physMemSize: number;
  readonly vmSize: number;
  readonly vmUsedCount: number;
  readonly __vmSwapUsage: number;
}

export interface ResourceUsage<
  ProcessAttribute extends keyof ProcessAttributes = never,
  SystemAttribute extends keyof SystemAttributes = never,
> {
  readonly time: number;
  readonly pageSize: number;
  /** Empty in the first sample. */
  readonly cpuLoadPerCore: readonly number[];
  readonly system: Pick<SystemAttributes, SystemAttribute>;
  readonly processes: readonly (Process &
    Pick<ProcessAttributes, ProcessAttribute>)[];
}

export type Signal =
  | "SIGHUP"
  | "SIGINT"
  | "SIGQUIT"
  | "SIGILL"
  | "SIGTRAP"
  | "SIGABRT"
  | "SIGEMT"
  | "SIGFPE"
  | "SIGKILL"
  | "SIGBUS"
  | "SIGSEGV"
  | "SIGSYS"
  | "SIGPIPE"
  | "SIGALRM"
  | "SIGTERM"
  | "SIGURG"
  | "SIGSTOP"
  | "SIGTSTP"
  | "SIGCONT"
  | "SIGCHLD"
  | "SIGTTIN"
  | "SIGTTOU"
  | "SIGIO"
  | "SIGXCPU"
  | "SIGXFSZ"
  | "SIGVTALRM"
  | "SIGPROF"
  | "SIGWINCH"
  | "SIGINFO"
  | "SIGUSR1"
  | "SIGUSR2";

export declare function list(options?: CallOptions): Promise<Process[]>;
export declare function resourceUsage<
  ProcessAttribute extends keyof ProcessAttributes = never,
  SystemAttribute extends keyof SystemAttributes = never,
>(
  options?: CallOptions & {
    readonly intervalMs?: number;
    readonly processAttributes?: readonly ProcessAttribute[];
    readonly systemAttributes?: readonly SystemAttribute[];
  },
): AsyncIterable<ResourceUsage<ProcessAttribute, SystemAttribute>>;
export declare function kill(
  pid: number,
  signal?: Signal | number,
  options?: CallOptions,
): Promise<void>;
