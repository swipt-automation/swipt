type Listener =
  ((event: never) => unknown) | { handleEvent(event: never): unknown };

export interface PerformanceHost {
  now(): number;
  timeOrigin: number;
  EventTarget: new () => {
    addEventListener(
      type: string,
      listener: Listener | null,
      options?: boolean | object,
    ): void;
    removeEventListener(
      type: string,
      listener: Listener | null,
      options?: boolean | object,
    ): void;
    dispatchEvent(event: never): boolean;
  };
  DOMException: new (message?: string, name?: string) => Error;
  structuredClone<T>(value: T): T;
}

export interface PerformanceMarkOptions {
  detail?: unknown;
  startTime?: number;
}

export interface PerformanceMeasureOptions {
  detail?: unknown;
  start?: string | number;
  duration?: number;
  end?: string | number;
}

export interface PerformanceObserverInit {
  entryTypes?: string[];
  type?: string;
  buffered?: boolean;
}

import {
  any,
  boolean,
  brand,
  callbackFunction,
  defineInterface,
  dictionary,
  dictionaryLike,
  domString,
  double,
  isObject,
  optional,
  sequence,
  type Converter,
} from "./webidl";

const supportedEntryTypes = Object.freeze(["mark", "measure"]);
const illegal = Symbol("illegal");

const markOrTime: Converter<string | number> = (value, context) =>
  typeof value === "number"
    ? double(value, context)
    : domString(value, context);
const markOptions = dictionary<PerformanceMarkOptions>({
  detail: any,
  startTime: double,
});
const measureOptions = dictionary<PerformanceMeasureOptions>({
  detail: any,
  duration: double,
  end: markOrTime,
  start: markOrTime,
});
const startOrMeasureOptions = dictionaryLike<
  string | PerformanceMeasureOptions
>((value, context) =>
  value === undefined || value === null || isObject(value)
    ? measureOptions(value, context)
    : domString(value, context),
);
const observerInit = dictionary<
  PerformanceObserverInit & { durationThreshold?: number }
>({
  buffered: boolean,
  durationThreshold: double,
  entryTypes: sequence(domString),
  type: domString,
});

export function createPerformance(host: PerformanceHost) {
  const { DOMException, structuredClone } = host;
  const timeline: PerformanceEntry[] = [];
  const observers: PerformanceObserver[] = [];
  let queued = false;

  let fieldsOf: (entry: PerformanceEntry) => {
    name: string;
    entryType: string;
    startTime: number;
  };
  const filter = (
    entries: readonly PerformanceEntry[],
    name?: string,
    type?: string,
  ) =>
    entries
      .filter(
        (entry) =>
          (name === undefined || fieldsOf(entry).name === name) &&
          (type === undefined || fieldsOf(entry).entryType === type),
      )
      .sort(
        (left, right) => fieldsOf(left).startTime - fieldsOf(right).startTime,
      );

  const detailOf = (detail: unknown): unknown =>
    detail === undefined || detail === null ? null : structuredClone(detail);

  const PerformanceEntry = defineInterface(
    class PerformanceEntry {
      readonly #name: string;
      readonly #entryType: string;
      readonly #startTime: number;
      readonly #duration: number;

      static [brand](value: object) {
        return #name in value;
      }

      static {
        fieldsOf = (entry) => ({
          name: entry.#name,
          entryType: entry.#entryType,
          startTime: entry.#startTime,
        });
      }

      constructor(
        token?: typeof illegal,
        name?: string,
        entryType?: string,
        startTime?: number,
        duration?: number,
      ) {
        if (token !== illegal) {
          throw new TypeError("Illegal constructor");
        }
        this.#name = name!;
        this.#entryType = entryType!;
        this.#startTime = startTime!;
        this.#duration = duration!;
      }

      get name(): string {
        return this.#name;
      }

      get entryType(): string {
        return this.#entryType;
      }

      get startTime(): number {
        return this.#startTime;
      }

      get duration(): number {
        return this.#duration;
      }

      toJSON() {
        return {
          name: this.#name,
          entryType: this.#entryType,
          startTime: this.#startTime,
          duration: this.#duration,
        };
      }
    },
    { operations: { toJSON: [] } },
  );
  type PerformanceEntry = InstanceType<typeof PerformanceEntry>;
  let createMark: (
    name: string,
    options: PerformanceMarkOptions,
  ) => PerformanceEntry;
  let createMeasure: (
    name: string,
    startTime: number,
    duration: number,
    detail: unknown,
  ) => PerformanceEntry;
  let createPerformanceObject: () => object;

  const PerformanceMark = defineInterface(
    class PerformanceMark extends PerformanceEntry {
      readonly #detail: unknown;

      static [brand](value: object) {
        return #detail in value;
      }

      static {
        createMark = (name, options) => new PerformanceMark(name, options);
      }

      constructor(markName: string, options?: PerformanceMarkOptions) {
        const { detail, startTime } = options!;
        const start = startTime ?? host.now();
        if (start < 0) {
          throw new TypeError("the start time is negative");
        }
        super(illegal, markName, "mark", start, 0);
        this.#detail = detailOf(detail);
      }

      get detail(): unknown {
        return this.#detail;
      }
    },
    { construct: [domString, optional(markOptions)] },
  );
  type PerformanceMark = InstanceType<typeof PerformanceMark>;

  const PerformanceMeasure = defineInterface(
    class PerformanceMeasure extends PerformanceEntry {
      readonly #detail: unknown;

      static [brand](value: object) {
        return #detail in value;
      }

      static {
        createMeasure = (name, startTime, duration, detail) =>
          new PerformanceMeasure(illegal, name, startTime, duration, detail);
      }

      constructor(
        token?: typeof illegal,
        name?: string,
        startTime?: number,
        duration?: number,
        detail?: unknown,
      ) {
        if (token !== illegal) {
          throw new TypeError("Illegal constructor");
        }
        super(illegal, name, "measure", startTime, duration);
        this.#detail = detail;
      }

      get detail(): unknown {
        return this.#detail;
      }
    },
    {},
  );
  type PerformanceMeasure = InstanceType<typeof PerformanceMeasure>;

  let createEntryList: (
    entries: readonly PerformanceEntry[],
  ) => PerformanceObserverEntryList;

  const PerformanceObserverEntryList = defineInterface(
    class PerformanceObserverEntryList {
      readonly #entries: readonly PerformanceEntry[];

      static [brand](value: object) {
        return #entries in value;
      }

      static {
        createEntryList = (entries) =>
          new PerformanceObserverEntryList(illegal, entries);
      }

      constructor(
        token?: typeof illegal,
        entries: readonly PerformanceEntry[] = [],
      ) {
        if (token !== illegal) {
          throw new TypeError("Illegal constructor");
        }
        this.#entries = entries;
      }

      getEntries(): PerformanceEntry[] {
        return filter(this.#entries);
      }

      getEntriesByType(type: string): PerformanceEntry[] {
        return filter(this.#entries, undefined, type);
      }

      getEntriesByName(name: string, type?: string): PerformanceEntry[] {
        return filter(this.#entries, name, type);
      }
    },
    {
      operations: {
        getEntries: [],
        getEntriesByType: [domString],
        getEntriesByName: [domString, optional(domString)],
      },
    },
  );
  type PerformanceObserverEntryList = InstanceType<
    typeof PerformanceObserverEntryList
  >;

  type Callback = (
    entries: PerformanceObserverEntryList,
    observer: PerformanceObserver,
    options: { droppedEntriesCount?: number },
  ) => void;
  interface Registration {
    entryTypes?: string[];
    type?: string;
  }

  let offer: (
    observer: PerformanceObserver,
    entry: PerformanceEntry,
  ) => boolean;
  let deliver: (observer: PerformanceObserver) => void;

  const notify = () => {
    queued = false;
    for (const observer of [...observers]) {
      deliver(observer);
    }
  };

  const schedule = () => {
    if (!queued) {
      queued = true;
      setTimeout(notify, 0);
    }
  };

  const PerformanceObserver = defineInterface(
    class PerformanceObserver {
      readonly #callback: Callback;
      #buffer: PerformanceEntry[] = [];
      #registrations: Registration[] = [];
      #mode: "multiple" | "single" | undefined;
      #dropped = true;

      static [brand](value: object) {
        return #callback in value;
      }

      static get supportedEntryTypes(): readonly string[] {
        return supportedEntryTypes;
      }

      static {
        offer = (observer, entry) => {
          const { entryType } = fieldsOf(entry);
          if (
            !observer.#registrations.some(
              (registration) =>
                registration.type === entryType ||
                registration.entryTypes?.includes(entryType),
            )
          ) {
            return false;
          }
          observer.#buffer.push(entry);
          return true;
        };
        deliver = (observer) => {
          if (observer.#buffer.length === 0) {
            return;
          }
          const list = createEntryList(observer.#buffer);
          observer.#buffer = [];
          const options: { droppedEntriesCount?: number } = {};
          if (observer.#dropped) {
            options.droppedEntriesCount = 0;
            observer.#dropped = false;
          }
          try {
            observer.#callback.call(observer, list, observer, options);
          } catch (error) {
            setTimeout(() => {
              throw error;
            }, 0);
          }
        };
      }

      constructor(callback: Callback) {
        this.#callback = callback;
      }

      observe(options?: PerformanceObserverInit): void {
        const { buffered, entryTypes: types, type: single } = options!;
        if (types === undefined && single === undefined) {
          throw new TypeError("observe needs entryTypes or type");
        }
        if (
          types !== undefined &&
          (single !== undefined || buffered !== undefined)
        ) {
          throw new TypeError(
            "entryTypes can't be combined with type or buffered",
          );
        }
        this.#mode ??= types !== undefined ? "multiple" : "single";
        if (this.#mode === "single" && types !== undefined) {
          throw new DOMException(
            "the observer already observes a single type",
            "InvalidModificationError",
          );
        }
        if (this.#mode === "multiple" && single !== undefined) {
          throw new DOMException(
            "the observer already observes a list of types",
            "InvalidModificationError",
          );
        }
        this.#dropped = true;
        if (types !== undefined) {
          const supported = types.filter((item) =>
            supportedEntryTypes.includes(item),
          );
          if (supported.length === 0) {
            return;
          }
          this.#registrations = [{ entryTypes: supported }];
        } else {
          if (!supportedEntryTypes.includes(single!)) {
            return;
          }
          const index = this.#registrations.findIndex(
            (registration) => registration.type === single,
          );
          this.#registrations.splice(
            index < 0 ? this.#registrations.length : index,
            1,
            { type: single },
          );
        }
        if (!observers.includes(this)) {
          observers.push(this);
        }
        if (single !== undefined && Boolean(buffered)) {
          this.#buffer.push(
            ...timeline.filter((entry) => fieldsOf(entry).entryType === single),
          );
          schedule();
        }
      }

      disconnect(): void {
        const index = observers.indexOf(this);
        if (index >= 0) {
          observers.splice(index, 1);
        }
        this.#buffer = [];
        this.#registrations = [];
      }

      takeRecords(): PerformanceEntry[] {
        const records = this.#buffer;
        this.#buffer = [];
        return records;
      }
    },
    {
      construct: [callbackFunction],
      operations: {
        observe: [optional(observerInit)],
        disconnect: [],
        takeRecords: [],
      },
    },
  );
  type PerformanceObserver = InstanceType<typeof PerformanceObserver>;

  const queue = <T extends PerformanceEntry>(entry: T): T => {
    let offered = false;
    for (const observer of observers) {
      offered = offer(observer, entry) || offered;
    }
    timeline.push(entry);
    if (offered) {
      schedule();
    }
    return entry;
  };

  const markTime = (mark: string | number): number => {
    if (typeof mark === "number") {
      if (mark < 0) {
        throw new TypeError("the timestamp is negative");
      }
      return mark;
    }
    const found = timeline.findLast(
      (entry) =>
        fieldsOf(entry).entryType === "mark" && fieldsOf(entry).name === mark,
    );
    if (found === undefined) {
      throw new DOMException(
        `the mark "${mark}" does not exist`,
        "SyntaxError",
      );
    }
    return fieldsOf(found).startTime;
  };

  const clear = (type: string, match: string | undefined) => {
    for (let index = timeline.length - 1; index >= 0; index--) {
      const { entryType, name } = fieldsOf(timeline[index]);
      if (entryType === type && (match === undefined || name === match)) {
        timeline.splice(index, 1);
      }
    }
  };

  const Performance = defineInterface(
    class Performance extends host.EventTarget {
      static [brand](value: object) {
        return #performance in value;
      }

      readonly #performance = true;

      static {
        createPerformanceObject = () => new Performance(illegal);
      }

      constructor(token?: typeof illegal) {
        if (token !== illegal) {
          throw new TypeError("Illegal constructor");
        }
        super();
      }

      get timeOrigin(): number {
        return host.timeOrigin;
      }

      now(): number {
        return host.now();
      }

      toJSON() {
        return { timeOrigin: host.timeOrigin };
      }

      mark(
        markName: string,
        markOptions?: PerformanceMarkOptions,
      ): PerformanceMark {
        return queue(createMark(markName, markOptions!)) as PerformanceMark;
      }

      clearMarks(markName?: string): void {
        clear("mark", markName);
      }

      measure(
        measureName: string,
        startOrMeasureOptions?: string | PerformanceMeasureOptions,
        endMark?: string,
      ): PerformanceMeasure {
        const name = measureName;
        const options =
          typeof startOrMeasureOptions === "string"
            ? undefined
            : startOrMeasureOptions!;
        const startMark =
          typeof startOrMeasureOptions === "string"
            ? startOrMeasureOptions
            : undefined;
        const { detail, duration, end, start } = options ?? {};
        const until = endMark;
        if (
          options !== undefined &&
          (start !== undefined ||
            end !== undefined ||
            duration !== undefined ||
            detail !== undefined)
        ) {
          if (until !== undefined) {
            throw new TypeError(
              "measure can't take both measure options and an end mark",
            );
          }
          if (start === undefined && end === undefined) {
            throw new TypeError("the measure options need a start or an end");
          }
          if (
            start !== undefined &&
            duration !== undefined &&
            end !== undefined
          ) {
            throw new TypeError(
              "the measure options can't have a start, a duration and an end together",
            );
          }
        }
        const endTime =
          until !== undefined
            ? markTime(until)
            : end !== undefined
              ? markTime(end)
              : start !== undefined && duration !== undefined
                ? markTime(start) + markTime(duration)
                : host.now();
        const startTime =
          start !== undefined
            ? markTime(start)
            : duration !== undefined && end !== undefined
              ? markTime(end) - markTime(duration)
              : startMark !== undefined
                ? markTime(startMark)
                : 0;
        return queue(
          createMeasure(
            name,
            startTime,
            endTime - startTime,
            options === undefined ? null : detailOf(detail),
          ),
        ) as PerformanceMeasure;
      }

      clearMeasures(measureName?: string): void {
        clear("measure", measureName);
      }

      getEntries(): PerformanceEntry[] {
        return filter(timeline);
      }

      getEntriesByType(type: string): PerformanceEntry[] {
        return filter(timeline, undefined, type);
      }

      getEntriesByName(name: string, type?: string): PerformanceEntry[] {
        return filter(timeline, name, type);
      }
    },
    {
      operations: {
        now: [],
        toJSON: [],
        mark: [domString, optional(markOptions)],
        clearMarks: [optional(domString)],
        measure: [
          domString,
          optional(startOrMeasureOptions),
          optional(domString),
        ],
        clearMeasures: [optional(domString)],
        getEntries: [],
        getEntriesByType: [domString],
        getEntriesByName: [domString, optional(domString)],
      },
    },
  );

  type Performance = InstanceType<typeof Performance>;

  return {
    performance: createPerformanceObject() as Performance,
    Performance,
    PerformanceEntry,
    PerformanceMark,
    PerformanceMeasure,
    PerformanceObserver,
    PerformanceObserverEntryList,
  };
}

export type PerformanceExports = ReturnType<typeof createPerformance>;
export type Performance = PerformanceExports["performance"];
