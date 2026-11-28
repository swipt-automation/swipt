import type { Client } from "../../../../client";
import type { Context } from "../../../command";
import { session } from "../../../session";

export async function watchServices(
  client: Client,
  id: string,
  signal: AbortSignal,
) {
  const { serviceText } = await import("../../../../output");
  for await (const response of client.swiplets.watchServices(
    { swipletId: id },
    { signal },
  )) {
    console.log(`${response.swipletId}: ${serviceText(response.service)}`);
  }
}

export const setService =
  (enabled: boolean) =>
  (context: Context<{ device?: string; watch?: boolean }>) =>
    session(context, async (client) => {
      const id = context.args[0];
      const [{ serviceText }, response] = await Promise.all([
        import("../../../../output"),
        client.swiplets.setServiceEnabled(
          { swipletId: id, enabled },
          { signal: context.signal },
        ),
      ]);
      if (context.options.watch) {
        await watchServices(client, id, context.signal);
        return;
      }
      console.log(`${id}: ${serviceText(response.service)}`);
    });
