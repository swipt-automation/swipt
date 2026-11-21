const outside = () => {
  throw new Error(
    "@swipt/api/android/applications runs inside Swipt; build the swiplet with `swipt project build`",
  );
};

export const installed = outside;
export const icon = outside;
export const launch = outside;
export const foreground = undefined;
export const events = outside;
