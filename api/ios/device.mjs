const outside = () => {
  throw new Error(
    "@swipt/api/ios/device runs inside Swipt; build the swiplet with `swipt project build`",
  );
};

export const info = outside;
export const screenshot = outside;
export const foregroundApplication = undefined;
export const locked = undefined;
export const displayState = undefined;
export const unlock = outside;
export const applicationEvents = outside;
export const installedApplications = outside;
export const applicationIcon = outside;
export const launchApplication = outside;
export const enableJIT = outside;
export const pasteboard = outside;
export const setPasteboard = outside;
export const simulatedLocation = undefined;
