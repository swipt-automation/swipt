import type { Rect } from "@swipt/api";
import type {
  AccessibilityAction,
  AccessibilityElement as AccessibilityElementInterface,
  AccessibilityRange,
  AccessibilitySelector,
  AccessibilitySnapshot as AccessibilitySnapshotInterface,
  AccessibilityWindow as AccessibilityWindowInterface,
  AccessibilityWindowType,
} from "@swipt/api/android/accessibility";

import { DOMException } from "../errors";
import { native } from "../native";
import { optionsOf, type Options } from "../options";
import { search as match } from "../selector";
import { act, call, displayOf, paramsOf, text } from "./call";
import { tap as touch } from "./input";

interface RawElement {
  handle: string;
  children: RawElement[];
  [field: string]: unknown;
}

interface RawWindow {
  root: RawElement | null;
  [field: string]: unknown;
}

interface RawSnapshot {
  time: number;
  seen: number;
  windows: RawWindow[];
}

interface Kept {
  id: string;
  snapshot: RawSnapshot;
}

type Selector = AccessibilitySelector;

const shownFlags = [
  "offScreen",
  "disabled",
  "focused",
  "selected",
  "secure",
  "heading",
  "editable",
] as const;

const handles = new WeakMap<AccessibilityElement, string>();
const homes = new WeakMap<AccessibilityElement, AccessibilitySnapshot>();
const ids = new WeakMap<AccessibilitySnapshot, string>();
const registries = new WeakMap<
  AccessibilitySnapshot,
  ReadonlyMap<string, AccessibilityElement>
>();

class AccessibilityElement implements AccessibilityElementInterface {
  declare readonly class?: string;
  declare readonly packageName?: string;
  declare readonly identifier?: string;
  declare readonly text?: string;
  declare readonly contentDescription?: string;
  declare readonly hint?: string;
  declare readonly stateDescription?: string;
  declare readonly error?: string;
  declare readonly paneTitle?: string;
  declare readonly tooltipText?: string;
  declare readonly frame: Rect;
  declare readonly offScreen?: true;
  declare readonly disabled?: true;
  declare readonly focused?: true;
  declare readonly selected?: true;
  declare readonly checked?: boolean;
  declare readonly secure?: true;
  declare readonly heading?: true;
  declare readonly editable?: true;
  declare readonly range?: AccessibilityRange;
  declare readonly selection?: [start: number, end: number];
  declare readonly collection?: {
    readonly rows: number;
    readonly columns: number;
  };
  declare readonly collectionItem?: {
    readonly row: number;
    readonly column: number;
  };
  declare readonly actions: readonly (AccessibilityAction | (string & {}))[];
  declare readonly children: readonly AccessibilityElement[];
  declare readonly parent?: AccessibilityElement;
  declare readonly window: AccessibilityWindow;

  find(selector: Selector): AccessibilityElement | null {
    return search(homes.get(this)!, handles.get(this)!, selector)[0] ?? null;
  }

  findAll(selector: Selector): AccessibilityElement[] {
    return search(homes.get(this)!, handles.get(this)!, selector);
  }

  toString(): string {
    return line(this);
  }
}

class AccessibilityWindow implements AccessibilityWindowInterface {
  declare readonly type: AccessibilityWindowType;
  declare readonly display: number;
  declare readonly layer: number;
  declare readonly frame: Rect;
  declare readonly title?: string;
  declare readonly packageName?: string;
  declare readonly focused?: true;
  declare readonly active?: true;
  declare readonly pictureInPicture?: true;
  declare readonly root: AccessibilityElement | null;

  toString(): string {
    const parts: string[] = [this.type];
    if (this.display !== 0) {
      parts.push(`display=${this.display}`);
    }
    if (this.packageName !== undefined) {
      parts.push(this.packageName);
    }
    if (this.title !== undefined) {
      parts.push(JSON.stringify(clip(this.title, 120)));
    }
    parts.push(`[${this.frame.join(",")}]`);
    for (const flag of ["focused", "active", "pictureInPicture"] as const) {
      if (this[flag]) {
        parts.push(flag);
      }
    }
    const header = parts.join(" ");
    return this.root === null ? header : `${header}\n${outline(this.root)}`;
  }
}

class AccessibilitySnapshot implements AccessibilitySnapshotInterface {
  declare readonly time: number;
  declare readonly windows: readonly AccessibilityWindow[];

  find(selector: Selector): AccessibilityElement | null {
    return search(this, "", selector)[0] ?? null;
  }

  findAll(selector: Selector): AccessibilityElement[] {
    return search(this, "", selector);
  }

  toString(): string {
    return this.windows.map(String).join("\n");
  }
}

const elementOf = (
  raw: RawElement,
  window: AccessibilityWindow,
  home: AccessibilitySnapshot,
  registry: Map<string, AccessibilityElement>,
  parent?: AccessibilityElement,
): AccessibilityElement => {
  const { handle, children, ...fields } = raw;
  const element = Object.assign(
    Object.create(AccessibilityElement.prototype),
    fields,
  ) as AccessibilityElement;
  handles.set(element, handle);
  homes.set(element, home);
  registry.set(handle, element);
  Object.defineProperty(element, "window", { value: window });
  if (parent !== undefined) {
    Object.defineProperty(element, "parent", { value: parent });
  }
  Object.defineProperty(element, "children", {
    value: Object.freeze(
      children.map((child) =>
        elementOf(child, window, home, registry, element),
      ),
    ),
    enumerable: true,
  });
  return Object.freeze(element);
};

const snapshotOf = ({ id, snapshot: raw }: Kept): AccessibilitySnapshot => {
  const home = Object.create(
    AccessibilitySnapshot.prototype,
  ) as AccessibilitySnapshot;
  const registry = new Map<string, AccessibilityElement>();
  ids.set(home, id);
  registries.set(home, registry);
  return Object.freeze(
    Object.assign(home, {
      time: raw.time,
      windows: Object.freeze(
        raw.windows.map(({ root, ...fields }) => {
          const window = Object.assign(
            Object.create(AccessibilityWindow.prototype),
            fields,
          ) as AccessibilityWindow;
          Object.defineProperty(window, "root", {
            value:
              root === null ? null : elementOf(root, window, home, registry),
            enumerable: true,
          });
          return Object.freeze(window);
        }),
      ),
    }),
  );
};

const refOf = (value: unknown): string | undefined =>
  value instanceof AccessibilityElement ? handles.get(value) : undefined;

const search = (
  home: AccessibilitySnapshot,
  scope: string,
  selector: Selector,
): AccessibilityElement[] => {
  const registry = registries.get(home)!;
  const element = (handle: unknown) => registry.get(handle as string)!;
  return match(ids.get(home)!, scope, selector, "all", {
    refOf,
    predicateArgument: element,
    adopt: element,
  });
};

const clip = (text: string, limit: number) =>
  text.length > limit
    ? `${text.slice(0, limit - limit / 3)}…${text.slice(-limit / 3)}`
    : text;

const labels = [
  ["contentDescription", "description"],
  ["hint", "hint"],
  ["stateDescription", "state"],
  ["error", "error"],
  ["paneTitle", "pane"],
  ["tooltipText", "tooltip"],
] as const;

const line = (element: AccessibilityElement): string => {
  const parts = [element.class ?? "element"];
  if (element.text !== undefined) {
    parts.push(JSON.stringify(clip(element.text, 300)));
  }
  for (const [field, label] of labels) {
    const value = element[field];
    if (value !== undefined) {
      parts.push(`${label}=${JSON.stringify(clip(value, 300))}`);
    }
  }
  if (element.identifier !== undefined) {
    parts.push(`id=${JSON.stringify(clip(element.identifier, 120))}`);
  }
  parts.push(`[${element.frame.join(",")}]`);
  for (const flag of shownFlags) {
    if (element[flag]) {
      parts.push(flag);
    }
  }
  if (element.checked !== undefined) {
    parts.push(element.checked ? "checked" : "unchecked");
  }
  if (element.range !== undefined) {
    const { type, min, max, current } = element.range;
    parts.push(`range=${type}:${current} in [${min},${max}]`);
  }
  if (element.selection !== undefined) {
    parts.push(`selection=[${element.selection.join(",")}]`);
  }
  if (element.actions.length > 0) {
    parts.push(`actions=${JSON.stringify(element.actions)}`);
  }
  return parts.join(" ");
};

const outline = (root: AccessibilityElement): string => {
  const lines: string[] = [];
  const visit = (element: AccessibilityElement, depth: number) => {
    lines.push("  ".repeat(depth) + line(element));
    for (const child of element.children) {
      visit(child, depth + 1);
    }
  };
  visit(root, 1);
  return lines.join("\n");
};

const identityOf = (element: unknown, operation: string) => {
  const handle = refOf(element);
  if (handle === undefined) {
    throw new TypeError(`${operation} takes an element of a snapshot`);
  }
  const read = element as AccessibilityElement;
  return {
    handle,
    class: read.class,
    identifier: read.identifier,
    text: read.text,
    contentDescription: read.contentDescription,
  };
};

const sameView = (
  element: AccessibilityElement,
  identity: ReturnType<typeof identityOf>,
) =>
  handles.get(element) === identity.handle &&
  element.class === identity.class &&
  element.identifier === identity.identifier &&
  element.contentDescription === identity.contentDescription &&
  (element.editable === true || element.text === identity.text);

const limitOf = (value: unknown): number => {
  if (value === undefined) {
    return 0;
  }
  if (value === Infinity) {
    return Infinity;
  }
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0) {
    throw new TypeError(
      "timeoutMs must be a non-negative number of milliseconds, or Infinity",
    );
  }
  return value;
};

const read = (options: Options | undefined, display: unknown) =>
  call<Kept>(
    "accessibility.snapshot",
    { display: displayOf(display) },
    options,
  );

export const snapshot = async (options?: Options) =>
  snapshotOf(await read(options, optionsOf(options, ["display"]).display));

const selectorsOf = (selector: unknown, operation: string): Selector[] => {
  const selectors = (
    Array.isArray(selector) ? selector : [selector]
  ) as Selector[];
  if (selectors.length === 0) {
    throw new TypeError(`${operation} needs a selector`);
  }
  return selectors;
};

const watch = async <T>(
  options: Options | undefined,
  answer: (taken: AccessibilitySnapshot) => T | undefined,
  limit: T,
): Promise<T> => {
  const { timeoutMs, display } = optionsOf(options, ["timeoutMs", "display"]);
  const deadline = Date.now() + limitOf(timeoutMs);
  for (;;) {
    const kept = await read(options, display);
    let found: T | undefined;
    try {
      found = answer(snapshotOf(kept));
    } finally {
      if (!(found instanceof AccessibilityElement)) {
        native.dropSnapshot(kept.id);
      }
    }
    if (found !== undefined) {
      return found;
    }
    const now = Date.now();
    if (now >= deadline) {
      return limit;
    }
    await call(
      "accessibility.changed",
      deadline === Infinity
        ? { seen: kept.snapshot.seen }
        : { seen: kept.snapshot.seen, timeoutMs: deadline - now },
      options,
    );
  }
};

export const find = (
  selector: unknown,
  options?: Options,
): Promise<AccessibilityElement | null> => {
  const selectors = selectorsOf(selector, "find");
  return watch<AccessibilityElement | null>(
    options,
    (taken) => {
      for (const item of selectors) {
        const element = taken.findAll(item)[0];
        if (element !== undefined) {
          return element;
        }
      }
      return undefined;
    },
    null,
  );
};

export const waitUntilGone = (target: unknown, options?: Options) => {
  if (target instanceof AccessibilityElement) {
    const identity = identityOf(target, "waitUntilGone");
    return watch(
      options,
      (taken) => {
        const shown = registries.get(taken)!.get(identity.handle);
        return shown !== undefined && sameView(shown, identity)
          ? undefined
          : true;
      },
      false,
    );
  }
  const selectors = selectorsOf(target, "waitUntilGone");
  return watch(
    options,
    (taken) =>
      selectors.some((item) => taken.findAll(item).length > 0)
        ? undefined
        : true,
    false,
  );
};

const defaultIdleMs = 500;
const defaultIdleTimeoutMs = 10_000;

export const waitForIdle = (options?: Options) => {
  const {
    idleMs = defaultIdleMs,
    timeoutMs = defaultIdleTimeoutMs,
    display,
  } = optionsOf(options, ["idleMs", "timeoutMs", "display"]);
  if (typeof idleMs !== "number" || !Number.isFinite(idleMs) || idleMs < 0) {
    throw new TypeError("idleMs must be a non-negative number of milliseconds");
  }
  const limit = limitOf(timeoutMs);
  return call<boolean>(
    "accessibility.waitForIdle",
    {
      idleMs,
      display: displayOf(display),
      ...(limit === Infinity ? {} : { timeoutMs: limit }),
    },
    options,
  );
};

interface Pending {
  pending: string;
  seen: number;
}

const aimed = async (
  timeoutMs: unknown,
  options: Options | undefined,
  attempt: () => Promise<Pending | undefined>,
) => {
  const limit = limitOf(timeoutMs);
  const deadline = Date.now() + limit;
  for (;;) {
    const answer = await attempt();
    if (answer === undefined) {
      return;
    }
    const now = Date.now();
    if (now >= deadline) {
      throw new DOMException(
        limit === 0
          ? answer.pending
          : `${answer.pending}; gave up after ${limit} ms`,
        "TimeoutError",
      );
    }
    await call(
      "accessibility.changed",
      deadline === Infinity
        ? { seen: answer.seen }
        : { seen: answer.seen, timeoutMs: deadline - now },
      options,
    );
  }
};

type Aim = { point: [x: number, y: number]; display: number } | Pending;

export const tap = (element: unknown, options?: Options) => {
  const identity = identityOf(element, "accessibility.tap");
  const { timeoutMs } = optionsOf(options, ["timeoutMs"]);
  return aimed(timeoutMs, options, async () => {
    const aim = await call<Aim>(
      "accessibility.aim",
      { element: identity },
      options,
    );
    if (!("point" in aim)) {
      return aim;
    }
    await touch(aim.point, {
      display: aim.display,
      signal: options?.signal,
    } as Options);
  });
};

export const setText = (
  element: unknown,
  value: unknown,
  options?: Options,
) => {
  const params = {
    element: identityOf(element, "accessibility.setText"),
    text: text(value, "the text"),
  };
  const { timeoutMs } = optionsOf(options, ["timeoutMs"]);
  return aimed(
    timeoutMs,
    options,
    async () =>
      (await call<Pending | null>("accessibility.setText", params, options)) ??
      undefined,
  );
};

const actionArguments: ReadonlyMap<string, readonly string[]> = new Map([
  ["setSelection", ["start", "end"]],
  ["setProgress", ["value"]],
  ["scrollToPosition", ["row", "column"]],
  ["moveWindow", ["x", "y"]],
]);

export const perform = (
  element: unknown,
  action: unknown,
  options?: unknown,
) => {
  const identity = identityOf(element, "accessibility.perform");
  const name = text(action, "the action");
  const names = actionArguments.get(name) ?? [];
  const given = paramsOf(options, [...names]);
  for (const key of names) {
    const value = given[key];
    if (key === "value" ? !Number.isFinite(value) : !Number.isInteger(value)) {
      throw new TypeError(
        `${name} takes ${key} as ${key === "value" ? "a finite number" : "an integer"}`,
      );
    }
  }
  return act(
    "accessibility.perform",
    names.length === 0
      ? { element: identity, action: name }
      : { element: identity, action: name, arguments: given },
    options as Options,
  );
};
