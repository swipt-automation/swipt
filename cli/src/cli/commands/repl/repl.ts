import { t } from "../../../i18n";
import { defineCommand } from "../../command";
import { data } from "../data";
import type { DataTarget } from "../data/operand";
import { deviceOption } from "../flags";
import { api } from "./api";
import { reset } from "./reset";
import { run } from "./run";

const replData = (args: string[]): DataTarget => ({
  id: "@repl",
  wholeDirectory: (name) => t("commands.repl.dataDirectory", { path: name }),
  subdirectory: (name) => t("commands.repl.directory", { path: name }),
  args,
});

export const repl = defineCommand({
  name: "repl",
  short: t("commands.repl.description"),
  options: deviceOption,
  commands: [run, reset, api, data("", replData)],
});
