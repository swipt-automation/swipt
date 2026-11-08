export const parseVersion = (value: unknown): number[] | undefined => {
  const text = typeof value === "number" ? String(value) : value;
  return typeof text === "string" && /^\d+(\.\d+)?$/.test(text)
    ? text.split(".").map(Number)
    : undefined;
};

export const versionAtLeast = (
  version: readonly number[],
  minimum: readonly number[],
): boolean => {
  for (const [index, part] of minimum.entries()) {
    const actual = version[index] ?? 0;
    if (actual !== part) {
      return actual > part;
    }
  }
  return true;
};
