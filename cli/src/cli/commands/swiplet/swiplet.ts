import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { data } from "../data";
import { deviceOption } from "../flags";
import { call } from "./call";
import { install } from "./install";
import { list } from "./list";
import { remove } from "./remove";
import { service } from "./service";
import { show } from "./show";

const swipletData = ([id, ...args]) => ({
  id,
  wholeDirectory: (name) =>
    tr(
      "{path} names the data directory of {swiplet}; save it with --output DIRECTORY",
      { path: name, swiplet: id },
    ),
  subdirectory: (name) =>
    tr(
      "{path} is a directory in the data directory of {swiplet}; save it with --output DIRECTORY",
      { path: name, swiplet: id },
    ),
  args,
});

export const swiplet = defineCommand({
  name: "swiplet",
  short: tr("Manage the swiplets on the device"),
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
