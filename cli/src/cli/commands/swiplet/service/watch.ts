import { tr } from "../../../../i18n";
import { defineCommand } from "../../../command";
import { session } from "../../../session";
import { watchServices } from "./services";

const serviceWatch = (context) =>
  session(context, (client) =>
    watchServices(client, context.args[0] ?? "", context.signal),
  );

export const watch = defineCommand({
  name: "watch",
  use: "watch [ID]",
  short: tr("Watch swiplet services"),
  args: [0, 1],
  run: serviceWatch,
});
