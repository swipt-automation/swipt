export const systems = { ios: "iOS", android: "Android" } as const;

export type OS = keyof typeof systems;

export const isOS = (name: string): name is OS => Object.hasOwn(systems, name);

export type Platform =
  | { readonly os: "ios"; readonly minimumVersion: readonly number[] }
  | { readonly os: "android"; readonly minimumSDK: number };
