export const platformModules: Record<string, () => unknown> = {
  "@expo/ui/jetpack-compose": () => require("@expo/ui/jetpack-compose"),
  "@expo/ui/jetpack-compose/modifiers": () =>
    require("@expo/ui/jetpack-compose/modifiers"),
};
