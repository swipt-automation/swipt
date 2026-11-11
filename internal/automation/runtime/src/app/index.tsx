import {
  registerRootComponent,
  requireNativeModule,
  type EventSubscription,
} from "expo";
import {
  Component,
  useEffect,
  useMemo,
  useSyncExternalStore,
  useState,
  type ComponentType,
  type ReactNode,
} from "react";

import { createAppIntents } from "../shared/app-intents";
import { decodeBase64 } from "../shared/base64";
import { inspect } from "../shared/inspect";
import {
  createSQLite,
  type DatabaseChangeEvent,
  type SQLiteBridge,
} from "../shared/sqlite";
import { sqliteHooks } from "./sqlite";

type ServiceStatus = "starting" | "running" | "stopped" | "failed";

interface ServiceEvent {
  enabled: boolean;
  status: ServiceStatus;
  error: string;
  since: number;
}

interface Service {
  enabled: boolean;
  status: ServiceStatus;
  error: string | null;
  since: Date | null;
}

interface Props {
  surface: string;
  name: string;
  file: string;
  source: string;
  dataDirectory: string;
  assetsDirectory: string;
  cacheDirectory: string;
  streams: string[];
  service: ServiceEvent;
}

interface SwiptHost {
  call(id: string, action: string, input: string): Promise<string>;
  cancelCall(id: string): Promise<void>;
  setServiceEnabled(enabled: boolean): Promise<void>;
  searchPlaces(query: string): Promise<string>;
  presentDialog(id: string, request: string): Promise<string | null>;
  withdrawDialog(id: string): Promise<void>;
  mounted(surface: string): void;
  exit(): void;
  preferredLocalizations(localizations: string[]): string[];
  log(level: string, message: string): void;
  fail(surface: string, message: string): void;
  report(message: string): void;
  sqlite(request: string): string;
  sqliteAsync(request: string): Promise<string>;
  addListener(
    event: "service",
    listener: (event: ServiceEvent) => void,
  ): EventSubscription;
  addListener(
    event: "sqlite",
    listener: (event: DatabaseChangeEvent) => void,
  ): EventSubscription;
  addListener(
    event: "value",
    listener: (event: { call: string; value?: string }) => void,
  ): EventSubscription;
}

const host = requireNativeModule<SwiptHost>("SwiptHost");

const describe = (error: unknown): string =>
  error instanceof Error
    ? String(error.stack ?? `${error.name}: ${error.message}`)
    : `Uncaught ${inspect(error)}`;

const messageOf = (error: unknown): string =>
  error instanceof Error ? error.message || error.name : inspect(error);

const format = (value: unknown): string =>
  typeof value === "string"
    ? value
    : value instanceof Error
      ? describe(value)
      : inspect(value);

(console as { reportErrorsAsExceptions?: boolean }).reportErrorsAsExceptions =
  false;
const web = require("./web") as {
  installWeb(host: SwiptHost, format: (value: unknown) => string): void;
  pathOfURI(uri: string): string;
  fileSystem(
    dataURI: string,
    assetsURI: string,
    cacheURI: string,
  ): Record<string, unknown>;
  Client: unknown;
  CookieJar: unknown;
  available: unknown;
};
web.installWeb(host, format);
Object.defineProperty(globalThis, "sleep", {
  value: (require("../extension/async") as { sleep: unknown }).sleep,
  writable: true,
  configurable: true,
  enumerable: false,
});

let service: Service = {
  enabled: false,
  status: "stopped",
  error: null,
  since: null,
};
const serviceListeners = new Set<() => void>();

const serviceOf = (event: ServiceEvent): Service => ({
  enabled: Boolean(event.enabled),
  status: event.status,
  error: event.error || null,
  since: event.since > 0 ? new Date(event.since) : null,
});

host.addListener("service", (event) => {
  service = serviceOf(event);
  for (const listener of serviceListeners) {
    listener();
  }
});

const report = (error: unknown) => {
  host.log("error", describe(error));
  host.report(messageOf(error));
};

const fail = (surface: string, error: unknown) => {
  host.log("error", describe(error));
  host.fail(surface, messageOf(error));
};

const exceptions = (
  require("react-native/Libraries/Core/ExceptionsManager") as {
    default: { handleException(error: unknown, isFatal: boolean): void };
  }
).default;

exceptions.handleException = (error, isFatal) => {
  if (isFatal || !(error as { isComponentError?: boolean }).isComponentError) {
    report(error);
  }
};

(
  HermesInternal as {
    enablePromiseRejectionTracker?: (options: {
      allRejections: boolean;
      onUnhandled: (id: number, error: unknown) => void;
    }) => void;
  } | null
)?.enablePromiseRejectionTracker?.({
  allRejections: true,
  onUnhandled: (_, error) => report(error),
});

const exportsOf = (specifier: string, values: Record<string, unknown>) =>
  new Proxy(
    { __esModule: true, ...values },
    {
      get: (target, key) => {
        if (typeof key === "string" && key !== "then" && !(key in target)) {
          throw new Error(`${specifier} has no export "${key}"`);
        }
        return Reflect.get(target, key);
      },
    },
  );

const abortError = (signal: AbortSignal): unknown =>
  signal.reason ??
  Object.assign(new Error("the call was aborted"), { name: "AbortError" });

const untilAborted = <T,>(
  signal: AbortSignal | undefined,
  start: () => Promise<T>,
): Promise<T> => {
  if (signal?.aborted) {
    return Promise.reject(abortError(signal));
  }
  const reply = start();
  if (signal === undefined) {
    return reply;
  }
  return new Promise((resolve, reject) => {
    const abort = () => reject(abortError(signal));
    signal.addEventListener("abort", abort, { once: true });
    reply
      .then(resolve, reject)
      .finally(() => signal.removeEventListener("abort", abort));
  });
};

const preferredLocalizations = (localizations: readonly string[]): string[] => {
  if (
    !Array.isArray(localizations) ||
    !localizations.every((localization) => typeof localization === "string")
  ) {
    throw new TypeError("the localizations must be an array of strings");
  }
  return host.preferredLocalizations([...localizations]);
};

let lastCall = 0;
const valueBacklog = 1000;
const valueListeners = new Map<string, (value?: string) => void>();

host.addListener("value", (event) => {
  valueListeners.get(event.call)?.(event.value);
});

function interfaceModule(props: Props) {
  service = serviceOf(props.service);
  const subscribe = (listener: () => void) => {
    serviceListeners.add(listener);
    return () => {
      serviceListeners.delete(listener);
    };
  };
  const streams = new Set(props.streams);
  const call = (
    action: string,
    input: unknown,
    { signal }: { signal?: AbortSignal } = {},
  ): Promise<unknown> | AsyncIterable<unknown> => {
    const id = String(++lastCall);
    const cancel = () => void host.cancelCall(id).catch(() => {});
    const buffered: unknown[] = [];
    let ended = false;
    let outcome: { error?: unknown } | undefined;
    let failure: { error: unknown } | undefined;
    let finished = false;
    const waiters: {
      resolve(result: IteratorResult<unknown>): void;
      reject(error: unknown): void;
    }[] = [];
    const advance = () => {
      if (failure !== undefined || ended) {
        valueListeners.delete(id);
      }
      while (waiters.length > 0) {
        if (finished) {
          waiters.shift()!.resolve({ value: undefined, done: true });
        } else if (buffered.length > 0) {
          waiters.shift()!.resolve({ value: buffered.shift(), done: false });
        } else if (failure !== undefined) {
          finished = true;
          waiters.shift()!.reject(failure.error);
        } else if (ended && outcome !== undefined) {
          finished = true;
          if ("error" in outcome) {
            waiters.shift()!.reject(outcome.error);
          }
        } else {
          return;
        }
      }
    };
    if (streams.has(action)) {
      valueListeners.set(id, (value) => {
        if (value === undefined) {
          ended = true;
        } else if (buffered.length < valueBacklog) {
          buffered.push(JSON.parse(value));
        } else {
          failure ??= {
            error: new Error(
              `the stream ended with ${valueBacklog} values unread`,
            ),
          };
          cancel();
        }
        advance();
      });
    }
    signal?.addEventListener("abort", cancel, { once: true });
    let started = false;
    const result = untilAborted(signal, async () => {
      const reply = host.call(id, action, JSON.stringify(input) ?? "null");
      started = true;
      return reply;
    })
      .then((output) => (output ? JSON.parse(output) : undefined))
      .finally(() => signal?.removeEventListener("abort", cancel));
    if (!streams.has(action)) {
      return result;
    }
    result
      .then(
        () => {
          outcome = {};
        },
        (error: unknown) => {
          if (signal?.aborted || !started) {
            failure ??= { error };
          }
          outcome = { error };
        },
      )
      .finally(advance);
    return {
      [Symbol.asyncIterator]: (): AsyncIterator<unknown> => ({
        next: () =>
          new Promise<IteratorResult<unknown>>((resolve, reject) => {
            waiters.push({ resolve, reject });
            advance();
          }),
        return: () => {
          failure ??= {
            error: Object.assign(new Error("the call was aborted"), {
              name: "AbortError",
            }),
          };
          buffered.length = 0;
          finished = true;
          cancel();
          advance();
          return Promise.resolve({ value: undefined, done: true });
        },
      }),
    };
  };
  const calls = new Proxy({} as Record<string, unknown>, {
    get: (_, action) =>
      typeof action !== "string" || action === "then"
        ? undefined
        : (input?: unknown, options?: { signal?: AbortSignal }) =>
            call(action, input, options),
  });
  const setEnabled = (enabled: boolean) =>
    host.setServiceEnabled(Boolean(enabled));
  const useService = () => {
    const current = useSyncExternalStore(subscribe, () => service);
    return useMemo(() => ({ ...current, setEnabled }), [current]);
  };
  return exportsOf("@swipt/api/swiplet", {
    actions: () => calls,
    useService,
    exit: () => host.exit(),
  });
}

let lastDialog = 0;

const appIntents = createAppIntents(
  async <T,>(
    request: Record<string, unknown>,
    _inApp: boolean,
    signal?: AbortSignal,
  ): Promise<T> => {
    const id = String(++lastDialog);
    const withdraw = () => void host.withdrawDialog(id).catch(() => {});
    signal?.addEventListener("abort", withdraw, { once: true });
    try {
      const answer = await untilAborted(signal, () =>
        host.presentDialog(id, JSON.stringify(request)),
      );
      if (answer === null) {
        throw Object.assign(new Error("the person dismissed the dialog"), {
          name: "AbortError",
        });
      }
      return JSON.parse(answer);
    } finally {
      signal?.removeEventListener("abort", withdraw);
    }
  },
  decodeBase64,
);

const modules: Record<string, () => unknown> = {
  react: () => require("react"),
  "react/jsx-runtime": () => require("react/jsx-runtime"),
  "react-native": () => require("react-native"),
  "react-native-worklets": () => require("react-native-worklets"),
  expo: () => require("expo"),
  "expo-localization": () => require("expo-localization"),
  "expo-maps": () => require("expo-maps"),
  "@expo/ui": () => require("@expo/ui"),
  "@expo/ui/swift-ui": () => require("@expo/ui/swift-ui"),
  "@expo/ui/swift-ui/modifiers": () => require("@expo/ui/swift-ui/modifiers"),
  "@expo/ui/community/bottom-sheet": () =>
    require("@expo/ui/community/bottom-sheet"),
  "@expo/ui/community/datetime-picker": () =>
    require("@expo/ui/community/datetime-picker"),
  "@expo/ui/community/masked-view": () =>
    require("@expo/ui/community/masked-view"),
  "@expo/ui/community/menu": () => require("@expo/ui/community/menu"),
  "@expo/ui/community/pager-view": () =>
    require("@expo/ui/community/pager-view"),
  "@expo/ui/community/picker": () => require("@expo/ui/community/picker"),
  "@expo/ui/community/segmented-control": () =>
    require("@expo/ui/community/segmented-control"),
  "@expo/ui/community/slider": () => require("@expo/ui/community/slider"),
  "@swipt/api/swift-ui": () =>
    exportsOf("@swipt/api/swift-ui", require("./swift-ui")),
};

const sqliteBridge = (props: Props): SQLiteBridge => ({
  request: (call) => JSON.parse(host.sqlite(JSON.stringify(call))),
  requestAsync: async (call) =>
    JSON.parse(await host.sqliteAsync(JSON.stringify(call))),
  defaultDirectory: web.pathOfURI(props.dataDirectory),
  subscribe(listener) {
    const subscription = host.addListener("sqlite", listener);
    return () => subscription.remove();
  },
});

let storage: ReturnType<typeof createSQLite> | undefined;

const installLocalStorage = (props: Props) => {
  storage ??= createSQLite(sqliteBridge(props));
  Object.defineProperty(globalThis, "localStorage", {
    value: storage.localStorage,
    configurable: true,
    enumerable: true,
    writable: true,
  });
  return { __esModule: true };
};

function load(props: Props): { Swiplet: ComponentType; close(): void } {
  const sqlite = createSQLite(sqliteBridge(props));
  let sqliteModule: object | undefined;
  const own: Record<string, () => unknown> = {
    "@swipt/api": () => exportsOf("@swipt/api", { available: web.available }),
    "@swipt/api/swiplet": () => interfaceModule(props),
    "@swipt/api/file-system": () =>
      exportsOf(
        "@swipt/api/file-system",
        web.fileSystem(
          props.dataDirectory,
          props.assetsDirectory,
          props.cacheDirectory,
        ),
      ),
    "@swipt/api/app-intents": () =>
      exportsOf("@swipt/api/app-intents", { ...appIntents }),
    "@swipt/api/localization": () =>
      exportsOf("@swipt/api/localization", { preferredLocalizations }),
    "@swipt/api/http": () =>
      exportsOf("@swipt/api/http", {
        Client: web.Client,
        CookieJar: web.CookieJar,
      }),
    "@swipt/api/maps": () =>
      exportsOf("@swipt/api/maps", {
        searchPlaces: (query: string, options: { signal?: AbortSignal } = {}) =>
          untilAborted(options.signal, async () =>
            JSON.parse(await host.searchPlaces(query)),
          ),
      }),
    "expo-sqlite": () =>
      (sqliteModule ??= Object.freeze({
        ...sqlite.exports,
        ...sqliteHooks(sqlite.exports),
      })),
    "expo-sqlite/kv-store": () => sqlite.kvStore,
    "expo-sqlite/localStorage/install": () => installLocalStorage(props),
  };
  const required = new Map<string, unknown>();
  const resolve = (specifier: string) => {
    if (required.has(specifier)) {
      return required.get(specifier);
    }
    const module = own[specifier] ?? modules[specifier];
    if (module === undefined) {
      throw new Error(
        `Swipt doesn't provide "${specifier}"; bundle it into ${props.file}`,
      );
    }
    const exports = module();
    required.set(specifier, exports);
    return exports;
  };
  const module: {
    exports: { default?: ComponentType } & Record<string, unknown>;
  } = { exports: {} };
  try {
    new Function(
      "require",
      "module",
      "exports",
      "reportError",
      `${props.source}\n//# sourceURL=swiplet://${props.name}/${props.file}`,
    )(resolve, module, module.exports, report);
  } catch (error) {
    sqlite.close();
    throw error;
  }
  return {
    Swiplet:
      module.exports.default ?? (module.exports as unknown as ComponentType),
    close: sqlite.close,
  };
}

class Boundary extends Component<
  { surface: string; children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(error: unknown) {
    fail(this.props.surface, error);
  }

  render() {
    return this.state.failed ? null : this.props.children;
  }
}

function Root(props: Props) {
  const [loaded] = useState(
    (): ReturnType<typeof load> | { error: unknown } => {
      try {
        return load(props);
      } catch (error) {
        return { error };
      }
    },
  );
  useEffect(() => {
    if ("error" in loaded) {
      fail(props.surface, loaded.error);
    }
    host.mounted(props.surface);
  }, [loaded, props.surface]);
  useEffect(
    () => () => ("close" in loaded ? loaded.close() : undefined),
    [loaded],
  );
  if ("error" in loaded) {
    return null;
  }
  const { Swiplet } = loaded;
  return (
    <Boundary surface={props.surface}>
      <Swiplet />
    </Boundary>
  );
}

registerRootComponent(Root);
