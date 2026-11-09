import { tr } from "../../../i18n";
import {
  columns,
  serviceEnabledText,
  serviceStatusText,
} from "../../../output";
import { defineCommand } from "../../command";
import { session } from "../../session";

const listSwiplets = (context) =>
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
          tr("ID"),
          tr("TITLE"),
          tr("VERSION"),
          tr("ACTIONS"),
          tr("SERVICE"),
          tr("STATUS"),
        ],
        ...rows,
      ]),
    );
  });

export const list = defineCommand({
  name: "list",
  short: tr("List the swiplets"),
  args: [0, 0],
  run: listSwiplets,
});
