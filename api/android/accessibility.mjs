const outside = () => {
  throw new Error(
    "@swipt/api/android/accessibility runs inside Swipt; build the swiplet with `swipt project build`",
  );
};

export const snapshot = outside;
export const find = outside;
export const waitUntilGone = outside;
export const waitForIdle = outside;
export const tap = outside;
export const setText = outside;
export const perform = outside;
