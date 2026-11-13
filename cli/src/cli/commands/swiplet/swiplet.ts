import { t } from "../../../i18n";
import { defineCommand } from "../../command";
import { data } from "../data";
import type { DataTarget } from "../data/operand";
import { deviceOption } from "../flags";
import { call } from "./call";
import { install } from "./install";
import { list } from "./list";
import { remove } from "./remove";
import { service } from "./service";
import { show } from "./show";

const swipletData = ([id, ...args]: string[]): DataTarget => ({
  id,
  wholeDirectory: (name) =>
    t("commands.swiplet.dataDirectory", { path: name, swiplet: id }),
  subdirectory: (name) =>
    t("commands.swiplet.directory", { path: name, swiplet: id }),
  args,
});

export const swiplet = defineCommand({
  name: "swiplet",
  short: t("commands.swiplet.description"),
  options: deviceOption,
  commands: [
    list,
    show,
    install,
    remove,
    call,
    service,
    data("ID ", swipletData),
  ],
});
