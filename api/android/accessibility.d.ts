/** @available apiLevel 4, android */
import type { CallOptions, Rect } from "../index";

export type AccessibilityWindowType =
  | "application"
  | "inputMethod"
  | "system"
  | "accessibilityOverlay"
  | "splitScreenDivider"
  | "magnificationOverlay"
  | "windowControl";

export type AccessibilityAction =
  | "click"
  | "longClick"
  | "contextClick"
  | "focus"
  | "clearFocus"
  | "select"
  | "clearSelection"
  | "accessibilityFocus"
  | "clearAccessibilityFocus"
  | "scrollForward"
  | "scrollBackward"
  | "scrollUp"
  | "scrollDown"
  | "scrollLeft"
  | "scrollRight"
  | "pageUp"
  | "pageDown"
  | "pageLeft"
  | "pageRight"
  | "showOnScreen"
  | "scrollToPosition"
  | "copy"
  | "paste"
  | "cut"
  | "setSelection"
  | "setProgress"
  | "expand"
  | "collapse"
  | "dismiss"
  | "imeEnter"
  | "pressAndHold"
  | "showTooltip"
  | "hideTooltip"
  | "moveWindow"
  | "dragStart"
  | "dragDrop"
  | "dragCancel"
  | "showTextSuggestions";

export interface AccessibilityActionArguments {
  readonly setSelection: { readonly start: number; readonly end: number };
  /** value is in the units of the element's range. */
  readonly setProgress: { readonly value: number };
  readonly scrollToPosition: { readonly row: number; readonly column: number };
  readonly moveWindow: { readonly x: number; readonly y: number };
}

export interface AccessibilityRange {
  readonly type: "integer" | "float" | "percent";
  readonly min: number;
  readonly max: number;
  readonly current: number;
}

/** The fields hold what the element showed when the snapshot was read. */
export interface AccessibilityElement {
  readonly class?: string;
  readonly packageName?: string;
  readonly identifier?: string;
  readonly text?: string;
  readonly contentDescription?: string;
  readonly hint?: string;
  readonly stateDescription?: string;
  readonly error?: string;
  readonly paneTitle?: string;
  readonly tooltipText?: string;
  /** In the coordinates of the display of its window, which input takes. */
  readonly frame: Rect;
  readonly offScreen?: true;
  readonly disabled?: true;
  readonly focused?: true;
  readonly selected?: true;
  readonly checked?: boolean;
  readonly secure?: true;
  readonly heading?: true;
  /** setText takes only an editable element. */
  readonly editable?: true;
  /** The range of a seek bar, a slider or a progress bar. */
  readonly range?: AccessibilityRange;
  readonly selection?: [start: number, end: number];
  /** The rows and columns of a list or a grid. */
  readonly collection?: { readonly rows: number; readonly columns: number };
  /** The position of an item of a collection. */
  readonly collectionItem?: { readonly row: number; readonly column: number };
  /** The actions the app offered for the element when the snapshot was read; an app can take an action it does not offer then, such as paste once the clipboard holds text. */
  readonly actions: readonly (AccessibilityAction | (string & {}))[];
  readonly children: readonly AccessibilityElement[];
  readonly parent?: AccessibilityElement;
  readonly window: AccessibilityWindow;
  /** Searches the descendants of the element as find of the snapshot does. */
  find(selector: AccessibilitySelector): AccessibilityElement | null;
  findAll(selector: AccessibilitySelector): AccessibilityElement[];
}

export interface AccessibilityWindow {
  readonly type: AccessibilityWindowType;
  readonly display: number;
  readonly layer: number;
  readonly frame: Rect;
  readonly title?: string;
  readonly packageName?: string;
  readonly focused?: true;
  readonly active?: true;
  readonly pictureInPicture?: true;
  readonly root: AccessibilityElement | null;
}

export interface AccessibilitySnapshot {
  /** The Unix time in milliseconds when the snapshot was read. */
  readonly time: number;
  /** The windows of each display, the default display first and the windows of each from the topmost down. */
  readonly windows: readonly AccessibilityWindow[];
  /** Answers the first element that matches in the order of findAll, or null. */
  find(selector: AccessibilitySelector): AccessibilityElement | null;
  /** Answers the matching elements of every window, the windows from the topmost down and the elements of each in depth-first order. */
  findAll(selector: AccessibilitySelector): AccessibilityElement[];
}

type TextField =
  | "class"
  | "packageName"
  | "identifier"
  | "text"
  | "contentDescription"
  | "hint"
  | "stateDescription"
  | "error"
  | "paneTitle"
  | "tooltipText";
type Flag =
  | "offScreen"
  | "disabled"
  | "focused"
  | "selected"
  | "checked"
  | "secure"
  | "heading"
  | "editable";

export type AccessibilitySelector =
  | ({
      windowType?: AccessibilityWindowType | RegExp;
      /** Matches an element that offers every action listed. */
      actions?: readonly (AccessibilityAction | (string & {}))[];
      within?: AccessibilitySelector | AccessibilityElement;
      parent?: AccessibilitySelector | AccessibilityElement;
      has?: AccessibilitySelector | AccessibilityElement;
      not?: AccessibilitySelector | AccessibilityElement;
      index?: number;
    } & { [field in TextField]?: string | RegExp } & {
      [field in Flag]?: boolean;
    })
  | ((element: AccessibilityElement) => boolean);

export interface SnapshotOptions extends CallOptions {
  /** Reads only the windows of the display with this ID; omitted reads every display. */
  readonly display?: number;
}

export interface FindOptions extends SnapshotOptions {
  /** Omitted or 0 reads the screen once; a positive number or Infinity reads it again on each change until the answer holds. */
  readonly timeoutMs?: number;
}

export interface IdleOptions extends CallOptions {
  /** How long the screen has to stay unchanged; 500 when omitted. */
  readonly idleMs?: number;
  /** 10000 when omitted; Infinity waits without a limit. */
  readonly timeoutMs?: number;
  /** Waits only on the windows of the display with this ID; omitted waits on every display. */
  readonly display?: number;
}

type ActionOptions<Action> = Action extends keyof AccessibilityActionArguments
  ? [options: CallOptions & AccessibilityActionArguments[Action]]
  : [options?: CallOptions];

/**
 * While Swipt is connected to the accessibility service of the device, AccessibilityManager reports accessibility as
 * enabled to every app.
 */
export declare function snapshot(
  options?: SnapshotOptions,
): Promise<AccessibilitySnapshot>;
/**
 * Answers the first element that matches in the order of findAll of the snapshot, so the topmost window comes first,
 * and system windows such as the status bar count; windowType narrows the search. Of an array, the first selector
 * that matches an element decides. Answers null when nothing matches by timeoutMs.
 */
export declare function find(
  selector: AccessibilitySelector | readonly AccessibilitySelector[],
  options?: FindOptions,
): Promise<AccessibilityElement | null>;
/**
 * Answers true once no element matches any of the selectors, or once the view of the element is gone from the
 * screen or shows other content; answers false when that has not happened by timeoutMs.
 */
export declare function waitUntilGone(
  target:
    | AccessibilitySelector
    | readonly AccessibilitySelector[]
    | AccessibilityElement,
  options?: FindOptions,
): Promise<boolean>;
/** Answers true once the windows and elements have gone idleMs without a change, and false at timeoutMs. */
export declare function waitForIdle(options?: IdleOptions): Promise<boolean>;

/**
 * An element stays usable while its view is on the screen and shows what it showed in the snapshot. Once the view is
 * gone or shows other content, as after the activity is recreated, the functions reject with NotFoundError at once;
 * find the element again.
 */
export interface ElementOptions extends CallOptions {
  /** How long to wait for the element to be enabled and to be uncovered by other windows, or for setText, to take the focus and the text. Omitted or 0 tries once, and rejects with TimeoutError when the element is not ready. */
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
export declare function perform<
  Action extends AccessibilityAction | (string & {}),
>(
  element: AccessibilityElement,
  action: Action,
  ...options: ActionOptions<Action>
): Promise<void>;
