declare global {
  const image: typeof import("@swipt/api/image");
  const dataDirectory: import("@swipt/api/file-system").DirectoryHandle;
  const args: readonly string[];

  function show(image: import("@swipt/api/image").RasterImage): void;
}

export {};
