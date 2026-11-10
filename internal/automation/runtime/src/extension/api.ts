import { find, perform, setText, snapshot, tap } from "./accessibility";
import * as deviceModule from "./device";
import { decode, handleOf, RasterImage } from "./image";
import * as inputModule from "./input";
import * as processModule from "./process";
import { open, openRecording } from "./screen";

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
export const image = Object.freeze({ decode });
export { handleOf, RasterImage };
