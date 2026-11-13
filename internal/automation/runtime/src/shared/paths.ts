export const joinRelative = (base: string, path: string): string => {
  if (typeof path !== "string") {
    throw new TypeError("the path must be a string");
  }
  if (path.startsWith("/")) {
    throw new TypeError(
      `${JSON.stringify(path)} is absolute; give a path inside the directory`,
    );
  }
  const segments = base === "" ? [] : base.split("/");
  for (const part of path.split("/")) {
    if (part === "" || part === ".") {
      continue;
    }
    if (part === "..") {
      if (segments.length === 0) {
        throw new TypeError(`${JSON.stringify(path)} leaves the directory`);
      }
      segments.pop();
      continue;
    }
    segments.push(part);
  }
  return segments.join("/");
};

export const nameOf = (path: string): string => path.split("/").at(-1) ?? "";

export const directoryURI = (absolute: string): string =>
  `file://${encodeURI(absolute.startsWith("/") ? absolute : `/${absolute}`).replace(/[?#]/g, encodeURIComponent)}`;

export const pathOfFileURI = (uri: string): string => {
  const path = decodeURIComponent(uri.slice("file://".length));
  return /^\/[A-Za-z]:\//.test(path) ? path.slice(1) : path;
};

export const fileURI = (
  root: string,
  path: string,
  directory: boolean,
): string => {
  const base = root.replace(/\/+$/, "");
  const encoded =
    path === "" ? "" : `/${path.split("/").map(encodeURIComponent).join("/")}`;
  return `${base}${encoded}${directory ? "/" : ""}`;
};
