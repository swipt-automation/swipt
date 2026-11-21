const outside = () => {
  throw new Error(
    "@swipt/api/android/notifications runs inside Swipt; build the swiplet with `swipt project build`",
  );
};

export const list = outside;
export const events = outside;
export const open = outside;
export const dismiss = outside;
export const perform = outside;
