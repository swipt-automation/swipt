import { errorFrom } from "./errors";
import { onAbort } from "./events";
import { native } from "./native";

interface Pending {
  resolve(value: unknown): void;
  reject(error: unknown): void;
  signal?: AbortSignal;
  release(): void;
  site: Error;
}

interface Subscription {
  push(value: unknown): void;
  fail(error: Error): void;
  complete(): void;
}

const pending = new Map<string, Pending>();
const subscriptions = new Map<string, Subscription>();
const backlog = 1000;
let sequence = 0;

const nextID = () => String(++sequence);

export interface CallOptions {
  signal?: AbortSignal;
}

export function invoke<T>(
  name: string,
  args: unknown[],
  options: CallOptions = {},
): Promise<T> {
  const { signal } = options;
  const site = new Error();
  return new Promise<T>((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason);
      return;
    }
    const id = nextID();
    const encoded = JSON.stringify(args);
    const release =
      signal === undefined
        ? () => {}
        : onAbort(signal, () => native.cancel(id));
    pending.set(id, {
      resolve: resolve as (value: unknown) => void,
      reject,
      signal,
      release,
      site,
    });
    native.start(id, name, encoded);
  });
}

export const sleep = (ms: number, options: CallOptions = {}): Promise<void> => {
  const { signal } = options;
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason);
      return;
    }
    const abort = () => {
      clearTimeout(timer);
      reject(signal!.reason);
    };
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", abort);
      resolve();
    }, ms);
    signal?.addEventListener("abort", abort, { once: true });
  });
};

export function settle(id: string): void {
  const entry = pending.get(id);
  if (entry === undefined) {
    return;
  }
  pending.delete(id);
  entry.release();
  const failure = native.failure(id);
  if (failure === "") {
    try {
      entry.resolve(native.value(id));
    } catch (error) {
      entry.reject(error);
    }
  } else if (entry.signal?.aborted) {
    entry.reject(entry.signal.reason);
  } else {
    const error = errorFrom(JSON.parse(failure));
    error.stack = entry.site.stack;
    entry.reject(error);
  }
}

export function stream<T>(
  kind: string,
  args: unknown[] | (() => unknown[]),
  signal: AbortSignal | undefined,
  keep: "latest" | "all",
  decode: (value: unknown) => T | undefined = (value) => value as T,
): AsyncIterable<T> {
  return {
    [Symbol.asyncIterator](): AsyncIterator<T> {
      const site = new Error();
      let id: string | undefined;
      let buffered: T[] = [];
      interface Waiter {
        resolve(result: IteratorResult<T>): void;
        reject(error: unknown): void;
      }
      let waiters: Waiter[] = [];
      let failure: unknown;
      let done = false;
      const finish = (
        settle: (first: Waiter) => void = (first) =>
          first.resolve({ value: undefined, done: true }),
      ): boolean => {
        done = true;
        signal?.removeEventListener("abort", abort);
        if (id !== undefined) {
          subscriptions.delete(id);
          native.unsubscribe(id);
          id = undefined;
        }
        const [first, ...rest] = waiters;
        waiters = [];
        if (first !== undefined) {
          settle(first);
        }
        for (const waiter of rest) {
          waiter.resolve({ value: undefined, done: true });
        }
        return first !== undefined;
      };
      const abort = () => {
        finish((first) => first.reject(signal!.reason));
      };
      const fail = (error: Error) => {
        error.stack = site.stack;
        if (!finish((first) => first.reject(error))) {
          failure = error;
        }
      };
      const open = () => {
        if (id !== undefined) {
          return;
        }
        const encoded = JSON.stringify(
          typeof args === "function" ? args() : args,
        );
        id = nextID();
        subscriptions.set(id, {
          push: (value) => {
            const decoded = decode(value);
            if (decoded === undefined) {
              return;
            }
            const waiter = waiters.shift();
            if (waiter) {
              waiter.resolve({ value: decoded, done: false });
            } else if (keep === "latest") {
              buffered = [decoded];
            } else if (buffered.length < backlog) {
              buffered.push(decoded);
            } else {
              fail(new Error(`the stream ended with ${backlog} events unread`));
            }
          },
          fail,
          complete: () => {
            finish();
          },
        });
        signal?.addEventListener("abort", abort);
        try {
          native.subscribe(id, kind, encoded, keep);
        } catch (error) {
          finish();
          throw error;
        }
      };
      return {
        next(): Promise<IteratorResult<T>> {
          if (signal?.aborted) {
            finish();
            return Promise.reject(signal.reason);
          }
          if (buffered.length > 0) {
            return Promise.resolve({ value: buffered.shift()!, done: false });
          }
          if (failure !== undefined) {
            const error = failure;
            failure = undefined;
            return Promise.reject(error);
          }
          if (done) {
            return Promise.resolve({ value: undefined, done: true });
          }
          open();
          return new Promise((resolve, reject) => {
            waiters.push({ resolve, reject });
          });
        },
        return(): Promise<IteratorResult<T>> {
          buffered = [];
          finish();
          return Promise.resolve({ value: undefined, done: true });
        },
      };
    },
  };
}

export function deliver(id: string, json: string): void {
  subscriptions.get(id)?.push(JSON.parse(json));
}

export function end(id: string, failure: string): void {
  const subscription = subscriptions.get(id);
  if (failure === "") {
    subscription?.complete();
  } else {
    subscription?.fail(errorFrom(JSON.parse(failure)));
  }
}
