import { chunk } from "./chunks";
import type { Declared, Table, UndeclaredExports } from "./extension";
import { appIntents } from "./ios/app-intents";

interface DeviceModules {
  "@swipt/api/ios/accessibility": typeof import("@swipt/api/ios/accessibility");
  "@swipt/api/ios/input": typeof import("@swipt/api/ios/input");
  "@swipt/api/ios/device": typeof import("@swipt/api/ios/device");
  "@swipt/api/ios/process": typeof import("@swipt/api/ios/process");
  "@swipt/api/ios/screen": typeof import("@swipt/api/ios/screen");
  "@swipt/api/ios/app-intents": typeof import("@swipt/api/ios/app-intents");
}

export const deviceModules = {
  "@swipt/api/ios/accessibility": () => ({
    __esModule: true,
    ...chunk("api").accessibility,
  }),
  "@swipt/api/ios/input": () => ({ __esModule: true, ...chunk("api").input }),
  "@swipt/api/ios/device": () => ({ __esModule: true, ...chunk("api").device }),
  "@swipt/api/ios/process": () => ({
    __esModule: true,
    ...chunk("api").process,
  }),
  "@swipt/api/ios/screen": () => ({ __esModule: true, ...chunk("api").screen }),
  "@swipt/api/ios/app-intents": () => ({ __esModule: true, ...appIntents }),
} satisfies Table<DeviceModules>;

type UndeclaredDeviceExports = Declared<
  UndeclaredExports<DeviceModules, typeof deviceModules>
>;

export const deviceGlobals = () => {
  const { accessibility, input, device, screen } = chunk("api");
  return { accessibility, input, device, screen, appIntents };
};
