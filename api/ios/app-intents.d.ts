/**
 * This package is designed to provide the same headless interactions as Shortcuts or App Intents.
 *
 * When Shortcuts runs an action, iOS presents these interactions itself, except requestValue.
 * In every other case Swipt presents them. If another app is in front, Swipt takes a screenshot
 * of it, comes to the front with the interaction over that screenshot, and returns to that app
 * when the interaction ends. Cancelling an interaction rejects it with AbortError.
 *
 * It works in every environment, including the REPL, but should not be used in place of native
 * UI interactions in a swiplet.
 * @available apiLevel 4, iOS
 */
import type { CallOptions } from "../index";

export type IntentChoiceStyle = "default" | "destructive" | "cancel";

export type ConfirmationActionName =
  | "add"
  | "addData"
  | "book"
  | "buy"
  | "call"
  | "checkIn"
  | "continue"
  | "create"
  | "do"
  | "download"
  | "filter"
  | "find"
  | "get"
  | "go"
  | "log"
  | "open"
  | "order"
  | "pay"
  | "play"
  | "playSound"
  | "post"
  | "request"
  | "run"
  | "search"
  | "send"
  | "set"
  | "share"
  | "start"
  | "startNavigation"
  | "toggle"
  | "turnOff"
  | "turnOn"
  | "view";

export interface Person {
  readonly name: string;
  readonly handle?: { type: "phoneNumber" | "emailAddress"; value: string };
}

export interface Placemark {
  readonly name?: string;
  readonly coordinate?: [latitude: number, longitude: number];
  readonly address?: string;
}

/**
 * A choice of style cancel rejects with AbortError. Swiping the request away answers
 * the first choice, so the first choice is the one that is safe to take unasked.
 * @available iOS 26
 */
export declare function requestChoice<
  T extends string | { title: string; style?: IntentChoiceStyle },
>(choices: T[], prompt: string, options?: CallOptions): Promise<T>;
/** The deny button rejects with AbortError. */
export declare function requestConfirmation(
  prompt: string,
  options?: CallOptions & {
    actionName?:
      | ConfirmationActionName
      | { accept: string; deny: string; destructive?: boolean };
  },
): Promise<void>;
export declare function requestValue(
  type: "text",
  prompt?: string,
  options?: CallOptions & {
    multiline?: boolean;
    /** Turns off autocapitalization, autocorrection, spell checking and smart punctuation. */
    literal?: boolean;
  },
): Promise<string>;
export declare function requestValue(
  type: "integer",
  prompt?: string,
  options?: CallOptions & { stepper?: boolean; min?: number; max?: number },
): Promise<number>;
export declare function requestValue(
  type: "number",
  prompt?: string,
  options?: CallOptions & { min?: number; max?: number },
): Promise<number>;
export declare function requestValue(
  type: "date" | "time" | "dateTime",
  prompt?: string,
  options?: CallOptions,
): Promise<Date>;
export declare function requestValue(
  type: "file",
  prompt?: string,
  options?: CallOptions,
): Promise<{ name: string; type: string; bytes: Uint8Array }>;
export declare function requestValue(
  type: "person",
  prompt?: string,
  options?: CallOptions,
): Promise<Person>;
export declare function requestValue(
  type: "placemark",
  prompt?: string,
  options?: CallOptions,
): Promise<Placemark>;
export declare function requestDisambiguation(
  values: string[],
  prompt: string,
  options?: CallOptions,
): Promise<string>;
export declare function requestValueConfirmation(
  value: string,
  prompt: string,
  options?: CallOptions,
): Promise<boolean>;
