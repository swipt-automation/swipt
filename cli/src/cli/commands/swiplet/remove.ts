import { t } from "../../../i18n";
import { type Context, defineCommand } from "../../command";
import { session } from "../../session";

const removeSwiplet = (context: Context<{ device?: string }>) =>
  session(context, async (client) => {
    await client.swiplets.removeSwiplet(
      { swipletId: context.args[0] },
      { signal: context.signal },
    );
    console.log(
      t("commands.swiplet.remove.removed", { name: context.args[0] }),
    );
  });

export const remove = defineCommand({
  name: "remove",
  use: "remove ID",
  short: t("commands.swiplet.remove.description"),
  args: [1, 1],
  run: removeSwiplet,
});
