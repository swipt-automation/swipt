const outside = () => {
  throw new Error(
    "@swipt/api/android/shell runs inside Swipt; build the swiplet with `swipt project build`",
  );
};

export const exec = outside;
export const spawn = outside;
