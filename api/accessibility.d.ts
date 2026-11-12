/** @available iOS 26 */
import type { CallOptions, Point, Rect } from "./index";

export type AccessibilityTrait =
  | "button"
  | "link"
  | "image"
  | "playsSound"
  | "keyboardKey"
  | "staticText"
  | "summaryElement"
  | "updatesFrequently"
  | "searchField"
  | "startsMediaSession"
  | "adjustable"
  | "allowsDirectInteraction"
  | "causesPageTurn"
  | "tabBar"
  | "header"
  | "webContent"
  | "textEntry";

export type AccessibilityAction =
  | "press"
  | "increment"
  | "decrement"
  | "escape"
  | "menu"
  | "scrollUp"
  | "scrollDown"
  | "scrollLeft"
  | "scrollRight"
  | "scrollToVisible";

export interface AccessibilityElement {
  readonly role: string;
  readonly class?: string;
  readonly traits: readonly AccessibilityTrait[];
  readonly label?: string;
  readonly value?: string;
  readonly identifier?: string;
  readonly placeholder?: string;
  readonly hint?: string;
  readonly url?: string;
  readonly frame?: Rect;
  readonly point?: Point;
  readonly partial?: true;
  readonly offScreen?: true;
  readonly inert?: true;
  readonly remote?: true;
  readonly disabled?: true;
  readonly focused?: true;
  readonly selected?: true;
  readonly expanded?: boolean;
  readonly secure?: true;
  readonly selection?: [location: number, length: number];
  readonly actions?: readonly string[];
  readonly truncated?: true;
  readonly children: readonly AccessibilityElement[];
  readonly parent?: AccessibilityElement;
  find(selector: AccessibilitySelector): AccessibilityElement | null;
  findAll(selector: AccessibilitySelector): AccessibilityElement[];
}

export interface AccessibilitySnapshot {
  readonly app: string;
  readonly time: number;
  readonly root: AccessibilityElement;
  readonly system?: AccessibilityElement;
  readonly truncated?: true;
  find(selector: AccessibilitySelector): AccessibilityElement | null;
  findAll(selector: AccessibilitySelector): AccessibilityElement[];
}

type TextField =
  | "role"
  | "class"
  | "label"
  | "value"
  | "identifier"
  | "placeholder"
  | "hint"
  | "url"
  | "app";
type Flag =
  | "disabled"
  | "focused"
  | "selected"
  | "expanded"
  | "secure"
  | "partial"
  | "offScreen"
  | "inert"
  | "remote"
  | "truncated";

export type AccessibilitySelector =
  | ({
      traits?: AccessibilityTrait[];
      within?: AccessibilitySelector | AccessibilityElement;
      parent?: AccessibilitySelector | AccessibilityElement;
      has?: AccessibilitySelector | AccessibilityElement;
      not?: AccessibilitySelector | AccessibilityElement;
      index?: number;
    } & { [field in TextField]?: string | RegExp } & {
      [field in Flag]?: boolean;
    })
  | ((element: AccessibilityElement) => boolean);

export interface ReadOptions extends CallOptions {
  readonly depth?: number;
}

export interface SnapshotOptions extends ReadOptions {
  readonly scope?: "frontApp" | "springBoard" | "fullScreen";
  readonly within?: AccessibilityElement;
  readonly full?: boolean;
}

export interface FindOptions extends ReadOptions {
  /** Omitted or 0 reads once; a positive number or Infinity reads until a match, and answers null at the limit. */
  readonly timeoutMs?: number;
  /** Reads again as soon as a read ends. Without it, a read waits for the device's next screen push, which comes 0.75 s after a change at the soonest and not at all for a change that adds or removes no element. */
  readonly poll?: boolean;
  readonly pollTimeoutMs?: number;
}

export declare function snapshot(
  options?: SnapshotOptions,
): Promise<AccessibilitySnapshot>;
export declare function find(
  selector: AccessibilitySelector | readonly AccessibilitySelector[],
  options?: FindOptions,
): Promise<AccessibilityElement | null>;
export interface ElementOptions extends CallOptions {
  readonly timeoutMs?: number;
}

export declare function tap(
  element: AccessibilityElement,
  options?: ElementOptions,
): Promise<void>;
export declare function setText(
  element: AccessibilityElement,
  text: string,
  options?: ElementOptions,
): Promise<void>;
export declare function perform(
  element: AccessibilityElement,
  action: AccessibilityAction | (string & {}),
  options?: CallOptions,
): Promise<void>;
