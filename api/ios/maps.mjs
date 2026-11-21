const outside = () => {
  throw new Error(
    "@swipt/api/ios/maps runs inside Swipt; build the swiplet with `swipt project build`",
  );
};

export const searchPlaces = outside;
