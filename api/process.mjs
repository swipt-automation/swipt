const outside = () => {
  throw new Error(
    "@swipt/api/process runs inside Swipt; build the swiplet with `swipt project build`",
  );
};

export const list = outside;
export const resourceUsage = outside;
export const kill = outside;
