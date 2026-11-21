import { find, perform, setText, snapshot, tap } from "./ios/accessibility";
import * as deviceModule from "./ios/device";
import * as inputModule from "./ios/input";
import * as processModule from "./ios/process";
import { open, openRecording } from "./ios/screen";

export { handleOf, image, RasterImage } from "./image";

export const accessibility = Object.freeze({
  snapshot,
  find,
  tap,
  setText,
  perform,
});
export const input = Object.freeze({ ...inputModule });
export const device = Object.freeze({ ...deviceModule });
export const process = Object.freeze({ ...processModule });
export const screen = Object.freeze({ open, openRecording });
