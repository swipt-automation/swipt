import { t } from "../../i18n";
import type { Options } from "../command";

export const deviceOption: Options = {
  device: {
    type: "string",
    short: "d",
    value: "NAME|URL",
    help: t("flags.device"),
  },
};

export const udidDeviceOption: Options = {
  device: {
    type: "string",
    short: "d",
    value: "NAME|UDID",
    help: t("flags.device"),
  },
};

export const sideloadOptions: Options = {
  device: {
    type: "string",
    short: "d",
    value: "NAME|ID",
    help: t("flags.device"),
  },
  team: {
    type: "string",
    value: "ID",
    help: t("flags.team"),
  },
};

export const passcodeOptions = (help: string): Options => ({
  passcode: { type: "string", value: "CODE", help },
  "passcode-type": {
    type: "string",
    value: "TYPE",
    help: t("flags.passcodeType"),
  },
});

export const sideloadPasscodeOptions = (help: string): Options => ({
  passcode: { type: "string", value: "CODE", help },
  "passcode-type": {
    type: "string",
    value: "TYPE",
    help: t("flags.sideloadPasscodeType"),
  },
});

export const nightlyOption: Options = {
  nightly: { type: "boolean", help: t("flags.nightly") },
};

export const setupStepOptions: Options = {
  "no-setup-pair": { type: "boolean", help: t("flags.noSetupPair") },
  "no-setup-api": {
    type: "boolean",
    help: t("flags.noSetupAPI"),
  },
};

export const setupOptions: Options = {
  "no-setup": {
    type: "boolean",
    help: t("flags.noSetup"),
  },
  ...setupStepOptions,
};
