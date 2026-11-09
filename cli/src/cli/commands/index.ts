import { tr } from "../../i18n";
import { defineCommand } from "../command";
import { device } from "./device";
import { project } from "./project";
import { repl } from "./repl";
import { sideload } from "./sideload";
import { swiplet } from "./swiplet";
import { version } from "./version";

export const root = defineCommand({
  name: "swipt",
  short: tr("Swipt CLI"),
  commands: [project, device, swiplet, repl, sideload, version],
});
