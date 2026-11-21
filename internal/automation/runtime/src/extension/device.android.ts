import { chunk } from "./chunks";
import type { Declared, Table, UndeclaredExports } from "./extension";

interface DeviceModules {
  "@swipt/api/android/accessibility": typeof import("@swipt/api/android/accessibility");
  "@swipt/api/android/input": typeof import("@swipt/api/android/input");
  "@swipt/api/android/applications": typeof import("@swipt/api/android/applications");
  "@swipt/api/android/notifications": typeof import("@swipt/api/android/notifications");
  "@swipt/api/android/device": typeof import("@swipt/api/android/device");
  "@swipt/api/android/shell": typeof import("@swipt/api/android/shell");
}

export const deviceModules = {
  "@swipt/api/android/accessibility": () => ({
    __esModule: true,
    ...chunk("api").accessibility,
  }),
  "@swipt/api/android/input": () => ({
    __esModule: true,
    ...chunk("api").input,
  }),
  "@swipt/api/android/applications": () => ({
    __esModule: true,
    ...chunk("api").applications,
  }),
  "@swipt/api/android/notifications": () => ({
    __esModule: true,
    ...chunk("api").notifications,
  }),
  "@swipt/api/android/device": () => ({
    __esModule: true,
    ...chunk("api").device,
  }),
  "@swipt/api/android/shell": () => ({
    __esModule: true,
    ...chunk("api").shell,
  }),
} satisfies Table<DeviceModules>;

type UndeclaredDeviceExports = Declared<
  UndeclaredExports<DeviceModules, typeof deviceModules>
>;

export const deviceGlobals = () => {
  const { accessibility, input, applications, notifications, device, shell } =
    chunk("api");
  return { accessibility, input, applications, notifications, device, shell };
};
