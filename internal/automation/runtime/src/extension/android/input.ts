import type { TouchSample } from "@swipt/api/android/input";

import { check, point, type Options } from "../options";
import { act, displayOf } from "./call";

const tapMs = 60;
const tapGapMs = 80;
const swipeMs = 250;
const dragMs = 600;
const dragSteps = 12;
const pinchMs = 500;
const pinchRadius = 100;

const touchOf = (
  fingers: readonly (readonly TouchSample[])[],
  given: Record<string, unknown>,
  options?: Options,
) =>
  act("input.touch", { fingers, display: displayOf(given.display) }, options);

const durationOf = (value: unknown, fallback: number): number => {
  if (value === undefined) {
    return fallback;
  }
  if (typeof value !== "number" || !(value > 0) || !Number.isFinite(value)) {
    throw new TypeError("durationMs must be a positive number");
  }
  return value;
};

export const tap = (at: unknown, options?: Options) => {
  const given = check(options, ["count", "display"]);
  const { count = 1 } = given;
  if (!Number.isInteger(count) || (count as number) < 1) {
    throw new TypeError("count must be a positive integer");
  }
  const [x, y] = point(at, "the point");
  const fingers = Array.from(
    { length: count as number },
    (_, index): TouchSample[] => {
      const start = index * (tapMs + tapGapMs);
      return [
        [x, y, start],
        [x, y, start + tapMs],
      ];
    },
  );
  return touchOf(fingers, given, options);
};

export const swipe = (from: unknown, to: unknown, options?: Options) => {
  const given = check(options, ["durationMs", "display"]);
  const duration = durationOf(given.durationMs, swipeMs);
  const [fromX, fromY] = point(from, "from");
  const [toX, toY] = point(to, "to");
  const fingers: TouchSample[][] = [
    [
      [fromX, fromY, 0],
      [toX, toY, duration],
    ],
  ];
  return touchOf(fingers, given, options);
};

export const drag = (from: unknown, to: unknown, options?: Options) => {
  const given = check(options, ["durationMs", "pressMs", "display"]);
  const { durationMs, pressMs = 0 } = given;
  const duration = durationOf(durationMs, dragMs);
  if (
    typeof pressMs !== "number" ||
    !(pressMs >= 0) ||
    !Number.isFinite(pressMs)
  ) {
    throw new TypeError("pressMs must be a non-negative number");
  }
  const [fromX, fromY] = point(from, "from");
  const [toX, toY] = point(to, "to");
  const path = Array.from({ length: dragSteps + 1 }, (_, step): TouchSample => {
    const progress = step / dragSteps;
    const eased = 1 - (1 - progress) ** 3;
    return [
      fromX + (toX - fromX) * eased,
      fromY + (toY - fromY) * eased,
      pressMs + duration * progress,
    ];
  });
  return touchOf([[[fromX, fromY, 0], ...path]], given, options);
};

export const pinch = (center: unknown, scale: unknown, options?: Options) => {
  const given = check(options, ["durationMs", "display"]);
  const duration = durationOf(given.durationMs, pinchMs);
  if (typeof scale !== "number" || !(scale > 0) || !Number.isFinite(scale)) {
    throw new TypeError("the scale must be a positive number");
  }
  const [x, y] = point(center, "the center");
  const from = pinchRadius / Math.min(scale, 1);
  const to = from * scale;
  const finger = (direction: number): TouchSample[] => [
    [x + direction * from, y, 0],
    [x + direction * to, y, duration],
  ];
  return touchOf([finger(-1), finger(1)], given, options);
};

export const touch = (fingers: unknown, options?: Options) => {
  const given = check(options, ["display"]);
  if (!Array.isArray(fingers) || fingers.length === 0) {
    throw new TypeError("the fingers must be a non-empty array of samples");
  }
  fingers.forEach((samples, index) => {
    if (!Array.isArray(samples) || samples.length === 0) {
      throw new TypeError(
        `finger ${index} must be a non-empty array of samples`,
      );
    }
    for (const sample of samples) {
      if (
        !Array.isArray(sample) ||
        sample.length !== 3 ||
        !sample.every(Number.isFinite)
      ) {
        throw new TypeError("a sample must be [x, y, ms] of finite numbers");
      }
    }
  });
  return touchOf(fingers as TouchSample[][], given, options);
};

export const pressKey = (key: unknown, options?: Options) => {
  const { display } = check(options, ["display"]);
  const keys = Array.isArray(key) ? key : [key];
  if (keys.length === 0 || !keys.every((it) => typeof it === "string")) {
    throw new TypeError(
      "the key must be a string, or a non-empty array of them",
    );
  }
  return act("input.key", { keys, display: displayOf(display) }, options);
};
