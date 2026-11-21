export const exportsOf = (specifier: string, values: Record<string, unknown>) =>
  new Proxy(
    { __esModule: true, ...values },
    {
      get: (target, key) => {
        if (typeof key === "string" && key !== "then" && !(key in target)) {
          throw new Error(`${specifier} has no export "${key}"`);
        }
        return Reflect.get(target, key);
      },
    },
  );
