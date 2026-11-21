import { inspect } from "../shared/inspect";
import { chunk } from "./chunks";
import { failureOf } from "./errors";
import { deviceGlobals } from "./device";
import { canonicalSpecifier, modules } from "./extension";
import { dataDirectory } from "./files";
import { jsonOf, setInputColumns } from "./format";
import { native } from "./native";

const show = (image: unknown): void => {
  native.show(chunk("api").handleOf(image, "show's argument").showBytes());
};

const outcome = (value: unknown): string => {
  if (value === undefined) {
    return "";
  }
  const { handleOf, RasterImage } = chunk("api");
  if (value instanceof RasterImage) {
    return JSON.stringify({ image: native.put(handleOf(value).showBytes()) });
  }
  return `{"text":${JSON.stringify(inspect(value))},"json":${jsonOf(value)}}`;
};

export const replGlobals = () => ({
  image: chunk("api").image,
  dataDirectory: dataDirectory(),
  show,
  ...deviceGlobals(),
});

const namespaces = new Map<string, object>();

const load = (specifier: string, name?: string): unknown => {
  const canonical = canonicalSpecifier(specifier);
  let namespace = namespaces.get(canonical);
  if (namespace === undefined) {
    const module = modules()[canonical];
    if (module === undefined) {
      throw new TypeError(
        `the REPL can't import ${JSON.stringify(specifier)}; it can import ${Object.keys(modules()).join(", ")}`,
      );
    }
    const exported = module() as { __esModule?: boolean };
    const descriptors: PropertyDescriptorMap =
      Object.getOwnPropertyDescriptors(exported);
    delete descriptors.__esModule;
    if (!exported.__esModule) {
      descriptors.default = { value: exported, enumerable: true };
    }
    descriptors[Symbol.toStringTag] = { value: "Module" };
    namespace = Object.freeze(Object.create(null, descriptors)) as object;
    namespaces.set(canonical, namespace);
  }
  if (name === undefined) {
    return namespace;
  }
  if (!(name in namespace)) {
    throw new SyntaxError(
      `the module ${JSON.stringify(specifier)} has no export named ${JSON.stringify(name)}`,
    );
  }
  return (namespace as Record<string, unknown>)[name];
};

const dynamicImport = async (specifier: unknown): Promise<unknown> =>
  load(String(specifier));

export function launch(
  script: (
    staticImport: typeof load,
    importCall: typeof dynamicImport,
  ) => Promise<unknown>,
  columns: [output: number, input: number][][],
  id: string,
  request: string,
): void {
  const { args = [], program }: { args?: string[]; program: boolean } =
    JSON.parse(request);
  setInputColumns(id, columns);
  Object.defineProperty(globalThis, "args", {
    value: args,
    writable: true,
    configurable: true,
    enumerable: false,
  });
  void script(load, dynamicImport)
    .then((value) => (program ? "" : outcome(value)))
    .then(
      (output) => native.finish("", output, id),
      (error) => native.finish(failureOf(error), "", id),
    );
}
