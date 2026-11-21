declare global {
  const accessibility: typeof import("@swipt/api/ios/accessibility");
  const input: typeof import("@swipt/api/ios/input");
  const device: typeof import("@swipt/api/ios/device");
  const appIntents: typeof import("@swipt/api/ios/app-intents");
  var screen: Screen;
  interface Screen extends Readonly<typeof import("@swipt/api/ios/screen")> {}
}

export {};
