/** @available apiLevel 4, android */
import type { CallOptions } from "../index";

export interface NotificationAction {
  readonly title: string;
  /** perform takes a reply for the action, and needs one. */
  readonly reply?: true;
}

export interface Notification {
  readonly key: string;
  readonly packageName: string;
  /** The Unix time in milliseconds when the app posted the notification. */
  readonly time: number;
  readonly title?: string;
  readonly text?: string;
  readonly subText?: string;
  readonly category?: string;
  readonly ongoing?: true;
  readonly groupSummary?: true;
  readonly actions: readonly NotificationAction[];
}

export interface NotificationEvent {
  /** posted also comes when the app updates a notification. */
  readonly type: "posted" | "removed";
  readonly notification: Notification;
  /** The Unix time in milliseconds when Swipt received the event. */
  readonly time: number;
}

export declare function list(options?: CallOptions): Promise<Notification[]>;
/** Collects the events from the call on, so the events before the iteration begins are kept. */
export declare function events(
  options?: CallOptions,
): AsyncIterable<NotificationEvent>;
/** Opens the notification as tapping it does. While the device is locked, the activity starts behind the keyguard. */
export declare function open(
  notification: Notification,
  options?: CallOptions,
): Promise<void>;
/** Rejects with NotAllowedError for an ongoing notification, which only its app can remove. */
export declare function dismiss(
  notification: Notification,
  options?: CallOptions,
): Promise<void>;
export declare function perform(
  notification: Notification,
  action: NotificationAction,
  options?: CallOptions & { reply?: string },
): Promise<void>;
