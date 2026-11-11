const outside = () => {
  throw new Error(
    "@swipt/api/swift-ui runs inside Swipt; build the swiplet with `swipt project build`",
  );
};

export const Chart = outside;
export const DataGrid = outside;
export const horizontalSizeClass = outside;
