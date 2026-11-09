import * as device from "../../../device";
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
    data("ID ", device.swipletData),
  ],
});
