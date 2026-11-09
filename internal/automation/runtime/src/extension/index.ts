import { deliver, end, settle, sleep } from "./async";
import { console } from "./console";
import { hooks } from "./extension";
import { describe, framesOf } from "./format";
import { bind, type Native } from "./native";
import { launch, replGlobals } from "./repl";
import { installGlobalScope } from "./scope";

const define = (values: Record<string, unknown>) => {
  for (const [name, value] of Object.entries(values)) {
    Object.defineProperty(globalThis, name, {
      value,
      writable: true,
      configurable: true,
      enumerable: false,
    });
  }
};

export function install(binding: Native) {
  bind(binding);
  installGlobalScope(console);
  define({ sleep } satisfies Pick<typeof globalThis, "sleep">);
  return {
    settle,
    deliver,
    end,
    describe,
    frames: framesOf,
    ...hooks,
    launch,
    repl: () => define(replGlobals()),
  };
}
