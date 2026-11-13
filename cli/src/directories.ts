import os from "node:os";
import path from "node:path";

const home = os.homedir();

export const configDirectory = path.join(
  process.platform === "win32"
    ? process.env.APPDATA || path.join(home, "AppData", "Roaming")
    : process.env.XDG_CONFIG_HOME || path.join(home, ".config"),
  "swipt",
);

export const cacheDirectory = path.join(
  process.platform === "win32"
    ? process.env.LOCALAPPDATA || path.join(home, "AppData", "Local")
    : process.env.XDG_CACHE_HOME || path.join(home, ".cache"),
  "swipt",
);
