import { exportsOf } from "./exports";

export const platformModules: Record<string, () => unknown> = {
  "expo-maps": () => require("expo-maps"),
  "@expo/ui/swift-ui": () => require("@expo/ui/swift-ui"),
  "@expo/ui/swift-ui/modifiers": () => require("@expo/ui/swift-ui/modifiers"),
  "@swipt/api/swift-ui": () =>
    exportsOf("@swipt/api/swift-ui", require("./swift-ui")),
  "@swipt/api/ios/maps": () =>
    exportsOf("@swipt/api/ios/maps", require("./maps")),
  "@swipt/api/ios/app-intents": () =>
    exportsOf(
      "@swipt/api/ios/app-intents",
      require("./app-intents").appIntents,
    ),
};
