import { requireNativeModule } from "expo";

import { createAppIntents } from "../shared/app-intents";
import { decodeBase64 } from "../shared/base64";
import { untilAborted } from "./abort";

const host = requireNativeModule<{
  presentDialog(id: string, request: string): Promise<string | null>;
  withdrawDialog(id: string): Promise<void>;
}>("SwiptHost");

let lastDialog = 0;

export const appIntents = createAppIntents(
  async <T>(
    request: Record<string, unknown>,
    _inApp: boolean,
    signal?: AbortSignal,
  ): Promise<T> => {
    const id = String(++lastDialog);
    const withdraw = () => void host.withdrawDialog(id).catch(() => {});
    signal?.addEventListener("abort", withdraw, { once: true });
    try {
      const answer = await untilAborted(signal, () =>
        host.presentDialog(id, JSON.stringify(request)),
      );
      if (answer === null) {
        throw Object.assign(new Error("the person dismissed the dialog"), {
          name: "AbortError",
        });
      }
      return JSON.parse(answer);
    } finally {
      signal?.removeEventListener("abort", withdraw);
    }
  },
  decodeBase64,
);
