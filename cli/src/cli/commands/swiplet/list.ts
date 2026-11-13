import { t } from "../../../i18n";
import {
  columns,
  serviceEnabledText,
  serviceStatusText,
} from "../../../output";
import { type Context, defineCommand } from "../../command";
import { session } from "../../session";

const listSwiplets = (context: Context<{ device?: string }>) =>
  session(context, async (client) => {
    const { swiplets } = await client.swiplets.listSwiplets(
      {},
      { signal: context.signal },
    );
    const rows = swiplets.map((swiplet) => {
      let service = "";
      let status = "";
      if (swiplet.service) {
        service = serviceEnabledText(swiplet.service.enabled);
        status = serviceStatusText(swiplet.service.status);
        if (swiplet.service.error) {
          status += `: ${swiplet.service.error.name}: ${swiplet.service.error.message}`;
        }
      }
      return [
        swiplet.id,
        swiplet.title,
        swiplet.version,
        swiplet.actions.map((action) => action.name).join(","),
        service,
        status,
      ];
    });
    console.log(
      columns([
        [
          t("commands.swiplet.list.id"),
          t("commands.swiplet.list.title"),
          t("commands.swiplet.list.version"),
          t("commands.swiplet.list.actions"),
          t("commands.swiplet.list.service"),
          t("commands.swiplet.list.status"),
        ],
        ...rows,
      ]),
    );
  });

export const list = defineCommand({
  name: "list",
  short: t("commands.swiplet.list.description"),
  args: [0, 0],
  run: listSwiplets,
});
