import { tr } from "../../../i18n";
import { defineCommand } from "../../command";
import { session } from "../../session";

const removeSwiplet = (context) =>
  session(context, async (client) => {
    await client.swiplets.removeSwiplet(
      { swipletId: context.args[0] },
      { signal: context.signal },
    );
    console.log(tr("removed {name}", { name: context.args[0] }));
  });

export const remove = defineCommand({
  name: "remove",
  use: "remove ID",
  short: tr("Remove a swiplet"),
  args: [1, 1],
  run: removeSwiplet,
});
