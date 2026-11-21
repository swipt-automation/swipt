import { createAppIntents } from "../../shared/app-intents";
import { invoke } from "../async";
import { native } from "../native";

export const appIntents = createAppIntents(
  (request, inApp, signal) => invoke("dialog", [request, inApp], { signal }),
  (data) => native.take(data),
);
