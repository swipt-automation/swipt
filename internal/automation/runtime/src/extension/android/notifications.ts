import type {
  Notification,
  NotificationAction,
  NotificationEvent,
} from "@swipt/api/android/notifications";

import { subscribe } from "../async";
import { check, signalOf } from "../options";
import { act, call, paramsOf } from "./call";

const keyOf = (notification: unknown, operation: string): string => {
  const key = (notification as Partial<Notification> | null)?.key;
  if (typeof key !== "string") {
    throw new TypeError(`${operation} takes a notification`);
  }
  return key;
};

export const list = (options?: unknown) =>
  call<Notification[]>(
    "notifications.list",
    paramsOf(options, []),
    options as never,
  );
export const events = (options?: unknown) =>
  subscribe<NotificationEvent>(
    "notifications.events",
    [],
    signalOf(check(options, [])),
    (value) => Object.freeze(value as NotificationEvent),
  );
export const open = (notification: unknown, options?: unknown) =>
  act(
    "notifications.open",
    { ...paramsOf(options, []), key: keyOf(notification, "open") },
    options as never,
  );
export const dismiss = (notification: unknown, options?: unknown) =>
  act(
    "notifications.dismiss",
    { ...paramsOf(options, []), key: keyOf(notification, "dismiss") },
    options as never,
  );
export const perform = (
  notification: unknown,
  action: unknown,
  options?: unknown,
) => {
  const key = keyOf(notification, "perform");
  const { actions } = notification as Notification;
  const index = Array.isArray(actions) ? actions.indexOf(action as never) : -1;
  if (index < 0) {
    throw new TypeError(
      "the action must be one of the actions of the notification",
    );
  }
  const { reply } = paramsOf(options, ["reply"]);
  if (reply !== undefined && typeof reply !== "string") {
    throw new TypeError("reply must be a string");
  }
  return act(
    "notifications.perform",
    { key, index, title: (action as NotificationAction).title, reply },
    options as never,
  );
};
