const outside = () => {
  throw new Error(
    "@swipt/api/ios/screen runs inside Swipt; build the swiplet with `swipt project build`",
  );
};

export const open = outside;
export const openRecording = outside;
