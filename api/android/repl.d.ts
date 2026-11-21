declare global {
  const accessibility: typeof import("@swipt/api/android/accessibility");
  const input: typeof import("@swipt/api/android/input");
  const applications: typeof import("@swipt/api/android/applications");
  const notifications: typeof import("@swipt/api/android/notifications");
  const device: typeof import("@swipt/api/android/device");
  const shell: typeof import("@swipt/api/android/shell");
}

export {};
