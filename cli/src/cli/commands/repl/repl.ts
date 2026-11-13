import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { data } from "../data";
import type { DataTarget } from "../data/operand";
import { deviceOption } from "../flags";
import { api } from "./api";
import { reset } from "./reset";
import { run } from "./run";

const replData = (args: string[]): DataTarget => ({
  id: "@repl",
  wholeDirectory: (name) =>
    tr(
      "{path} names the data directory of the REPL; save it with --output DIRECTORY",
      { path: name },
    ),
  subdirectory: (name) =>
    tr(
      "{path} is a directory in the data directory of the REPL; save it with --output DIRECTORY",
      { path: name },
    ),
  args,
});

export const repl = defineCommand({
  name: "repl",
  short: tr("Run JavaScript in the REPL session on the device"),
  options: deviceOption,
  commands: [run, reset, api, data("", replData)],
});
