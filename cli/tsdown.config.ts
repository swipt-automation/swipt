import { defineConfig } from "tsdown";

export default defineConfig({
  entry: ["src/*.ts"],
  unbundle: true,
  format: "esm",
  platform: "node",
  target: "node22",
  fixedExtension: false,
  dts: true,
  sourcemap: true,
});
