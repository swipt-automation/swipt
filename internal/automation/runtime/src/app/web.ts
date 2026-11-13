import { settle } from "../extension/async";
import type { Chunks } from "../extension/chunks";
import {
  assets,
  cacheDirectory,
  dataDirectory,
  type DirectoryHandle,
} from "../extension/files";
import { bind, type Native } from "../extension/native";
import { installGlobalScope } from "../extension/scope";

export { available } from "../extension/available";
export { Client, CookieJar } from "../extension/http";
import { createConsole } from "../shared/console";
import { inspect } from "../shared/inspect";

interface WebResult {
  data?: Uint8Array;
  json?: string;
  failure?: string;
}

export interface WebHost {
  readonly osVersion: string;
  languages(): string[];
  log(level: string, message: string): void;
  webStart(id: string, name: string, args: string): Promise<WebResult>;
  webCancel(id: string): void;
  webPut(data: Uint8Array): string;
  webRelease(id: string, reason: string): void;
  webOpenFile(root: string, path: string): string;
  webCompression(format: string, mode: string): string[];
  webRandom(length: number): Uint8Array;
  webDigest(algorithm: string, data: Uint8Array): Uint8Array;
  webDecodeText(
    data: Uint8Array,
    fatal: boolean,
  ): { text?: string; restored?: number };
  publishObjectURL(url: string, data: Uint8Array | null): void;
}

declare const require: (id: string) => never;

const chunks: { [Name in keyof Chunks]: () => Chunks[Name] } = {
  "encoding-tables": () => require("../shared/encoding-tables"),
  sqlite: () => require("../shared/sqlite"),
  subtle: () => require("../extension/subtle"),
  url: () => require("../extension/whatwg-url"),
  multipart: () => require("../extension/multipart"),
  api: () => {
    throw new Error("the device APIs are only available in the extension");
  },
};

const unavailable = (name: string) => () => {
  throw new Error(`${name} is only available in the extension`);
};

const directories = { data: "", assets: "", cache: "" };

export const pathOfURI = (uri: string): string =>
  decodeURIComponent(uri.replace(/^file:\/\//, "")).replace(/\/+$/, "");

export function fileSystem(
  dataURI: string,
  assetsURI: string,
  cacheURI: string,
): {
  dataDirectory: DirectoryHandle;
  cacheDirectory: DirectoryHandle;
  assets: DirectoryHandle;
} {
  directories.data = pathOfURI(dataURI);
  directories.assets = pathOfURI(assetsURI);
  directories.cache = pathOfURI(cacheURI);
  return {
    dataDirectory: dataDirectory(),
    cacheDirectory: cacheDirectory(),
    assets: assets(),
  };
}

export function installWeb(
  host: WebHost,
  format: (value: unknown) => string,
): void {
  const results = new Map<string, WebResult>();
  const {
    performance: timing,
    console: original,
    setTimeout,
    setInterval,
    clearTimeout,
    TextEncoder: Encoder,
  } = globalThis;
  const now = () => timing.now();
  const encoder = new Encoder();
  const native: Native = {
    swiplet: "",
    get dataDirectory() {
      return directories.data;
    },
    get assetsDirectory() {
      return directories.assets;
    },
    get cacheDirectory() {
      return directories.cache;
    },
    extensionHead: "",
    timeOrigin: String(Date.now() - now()),
    now,
    start(id, name, args) {
      host
        .webStart(id, name, args)
        .then(
          (result) => results.set(id, result),
          (error: unknown) =>
            results.set(id, {
              failure: JSON.stringify({
                name: "Error",
                message: error instanceof Error ? error.message : String(error),
              }),
            }),
        )
        .then(() => settle(id));
    },
    cancel: (id) => host.webCancel(id),
    failure(id) {
      const failure = results.get(id)?.failure;
      if (failure === undefined) {
        return "";
      }
      results.delete(id);
      return failure;
    },
    value(id) {
      const result = results.get(id);
      results.delete(id);
      return result?.data ?? JSON.parse(result?.json ?? "null");
    },
    subscribe: unavailable("subscribe"),
    unsubscribe: unavailable("unsubscribe"),
    print: (level, text) => host.log(level, text),
    put: (data) => host.webPut(data),
    take: unavailable("take"),
    random: (length) => host.webRandom(length),
    digest: (algorithm, data) => host.webDigest(algorithm, data),
    release: (id, reason) => host.webRelease(id, reason ?? ""),
    compression: (format, mode) =>
      host.webCompression(format, mode) as [string, string],
    encodeText: (text) => encoder.encode(text),
    decodeText(data, fatal) {
      const decoded = host.webDecodeText(data, fatal !== 0);
      return decoded.text ?? decoded.restored!;
    },
    chunk: (name) => () => chunks[name as keyof Chunks](),
    setTimeout,
    setInterval,
    clearTimer: (id) => clearTimeout(id),
    finish: unavailable("finish"),
    emit: unavailable("emit"),
    show: unavailable("show"),
    match: unavailable("match"),
    preferredLocalizations: unavailable("preferredLocalizations"),
    environment: () => ({
      osVersion: host.osVersion,
      languages: host.languages(),
    }),
    locate: () => undefined,
    sqlite: unavailable("sqlite"),
    openFile: (root, path) => host.webOpenFile(root, path),
    publishObjectURL: (url, data) => host.publishObjectURL(url, data),
  };
  Object.setPrototypeOf(native, null);
  bind(native);
  for (const name of [
    "fetch",
    "XMLHttpRequest",
    "WebSocket",
    "Headers",
    "Request",
    "Response",
  ]) {
    Reflect.deleteProperty(globalThis, name);
  }
  installGlobalScope(
    createConsole({
      print(severity, text) {
        original[severity](text);
        host.log(severity, text);
      },
      format,
      inspect,
      trace: (label) =>
        `Trace${label === "" ? "" : `: ${label}`}\n${String(
          new Error().stack ?? "",
        )
          .split("\n")
          .slice(2)
          .join("\n")}`,
      now,
    }),
  );
}
