const outside = () => {
  throw new Error(
    "@swipt/api/android/device runs inside Swipt; build the swiplet with `swipt project build`",
  );
};

export const info = outside;
export const displays = undefined;
export const createVirtualDisplay = outside;
export const releaseVirtualDisplay = outside;
export const screenshot = outside;
export const openScreenStream = outside;
export const openScreenRecording = outside;
export const clipboard = undefined;
export const locked = undefined;
export const unlock = outside;
export const lock = outside;
