import { tr } from "../../i18n";
import type { Options } from "../command";

export const deviceOption: Options = {
  device: {
    type: "string",
    short: "d",
    value: "NAME|URL",
    help: tr("the device to use"),
  },
};

export const sideloadOptions: Options = {
  device: {
    type: "string",
    short: "d",
    value: "NAME|UDID",
    help: tr("the device to use"),
  },
  team: {
    type: "string",
    value: "ID",
    help: tr("team ID, required if more than one"),
  },
};

export const passcodeOptions = (help): Options => ({
  passcode: { type: "string", value: "CODE", help },
  "passcode-type": {
    type: "string",
    value: "TYPE",
    help: tr("pin (4 or 6 digits, default), numeric or alphanumeric"),
  },
});

export const nightlyOption: Options = {
  nightly: { type: "boolean", help: tr("use nightly build") },
};

export const setupStepOptions: Options = {
  "no-setup-pair": { type: "boolean", help: tr("do not pair Swipt") },
  "no-setup-api": {
    type: "boolean",
    help: tr("do not publish the API to the local network"),
  },
};

export const setupOptions: Options = {
  "no-setup": {
    type: "boolean",
    help: tr("do not pair Swipt, trust its developer or publish its API"),
  },
  ...setupStepOptions,
};
