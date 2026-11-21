const outside = () => {
  throw new Error(
    "@swipt/api/ios/process runs inside Swipt; build the swiplet with `swipt project build`",
  );
};

export const list = outside;
export const resourceUsage = outside;
export const kill = outside;
