import { t } from "../../../../i18n";
import { type Context, defineCommand } from "../../../command";
import { session } from "../../../session";
import { watchServices } from "./services";

const serviceWatch = (context: Context<{ device?: string }>) =>
  session(context, (client) =>
    watchServices(client, context.args[0] ?? "", context.signal),
  );

export const watch = defineCommand({
  name: "watch",
  use: "watch [ID]",
  short: t("commands.swiplet.service.watch.description"),
  args: [0, 1],
  run: serviceWatch,
});
