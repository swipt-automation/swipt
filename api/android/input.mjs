const outside = () => {
  throw new Error(
    "@swipt/api/android/input runs inside Swipt; build the swiplet with `swipt project build`",
  );
};

export const tap = outside;
export const swipe = outside;
export const drag = outside;
export const pinch = outside;
export const touch = outside;
export const pressKey = outside;
