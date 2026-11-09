import * as device from "../../../device";
import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { data } from "../data";
import { deviceOption } from "../flags";
import { api } from "./api";
import { reset } from "./reset";
import { run } from "./run";

export const repl = defineCommand({
  name: "repl",
  short: tr("Run JavaScript in the REPL session on the device"),
  options: deviceOption,
  commands: [run, reset, api, data("", device.replData)],
});
