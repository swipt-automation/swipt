import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { build } from "./build";
import { create } from "./create";
import { deploy } from "./deploy";
import { develop } from "./develop";
import { source } from "./source";

export const project = defineCommand({
  name: "project",
  short: tr("Make, build and deploy swiplet projects"),
  commands: [create, build, source, deploy, develop],
});
