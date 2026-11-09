import { serviceText } from "../../../../output";
import { session } from "../../../session";

export async function watchServices(client, id, signal) {
  for await (const response of client.swiplets.watchServices(
    { swipletId: id },
    { signal },
  )) {
    console.log(
      `${response.swipletId}: ${serviceText(response.swipletId, response.service)}`,
    );
  }
}

export const setService = (enabled) => (context) =>
  session(context, async (client) => {
    const id = context.args[0];
    const response = await client.swiplets.setServiceEnabled(
      { swipletId: id, enabled },
      { signal: context.signal },
    );
    if (context.options.watch) {
      await watchServices(client, id, context.signal);
      return;
    }
    console.log(`${id}: ${serviceText(id, response.service)}`);
  });
