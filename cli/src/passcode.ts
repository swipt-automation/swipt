import password from "@inquirer/password";
import select from "@inquirer/select";

import { UsageError } from "./client";
import { t } from "./i18n";
import { errorMessage, selectTheme } from "./output";

export type PasscodePlatform = "ios" | "android";

export const passcodeTypes = ["pin", "numeric", "alphanumeric"];
export const androidPasscodeTypes = ["pin", "pattern", "password", "none"];
const allPasscodeTypes = [
  ...new Set([...passcodeTypes, ...androidPasscodeTypes]),
];

export interface Passcode {
  passcode: string;
  type: string;
}

export interface PasscodeOptions {
  passcode?: string;
  "passcode-type"?: string;
}

export function validatePasscodeType(options: PasscodeOptions) {
  const type = options["passcode-type"];
  if (type !== undefined && !allPasscodeTypes.includes(type)) {
    throw new UsageError(
      t("passcode.invalidType", {
        value: JSON.stringify(type),
        types: allPasscodeTypes.join(", "),
      }),
    );
  }
}

export function passcodeOf(
  options: PasscodeOptions,
  platform: PasscodePlatform = "ios",
): Passcode | undefined {
  const allowed = platform === "android" ? androidPasscodeTypes : passcodeTypes;
  const type = options["passcode-type"] ?? "pin";
  if (!allowed.includes(type)) {
    throw new UsageError(
      t("passcode.invalidType", {
        value: JSON.stringify(type),
        types: allowed.join(", "),
      }),
    );
  }
  const passcode = options.passcode;
  if (platform === "android") {
    if (type === "none") {
      if (passcode) {
        throw new UsageError(t("passcode.invalidNone"));
      }
      return options["passcode-type"] === "none"
        ? { passcode: "", type: "none" }
        : undefined;
    }
    if (!passcode) {
      return undefined;
    }
    if (type === "pin") {
      if (!/^\d{4,16}$/.test(passcode)) {
        throw new UsageError(t("passcode.invalidAndroidPin"));
      }
      return { passcode, type };
    }
    if (type === "pattern") {
      if (
        !/^[1-9]{4,9}$/.test(passcode) ||
        new Set(passcode).size !== passcode.length
      ) {
        throw new UsageError(t("passcode.invalidPattern"));
      }
      return { passcode, type };
    }
    if (!/^[\x20-\x7e]{4,16}$/.test(passcode)) {
      throw new UsageError(t("passcode.invalidPassword"));
    }
    return { passcode, type };
  }
  if (!passcode) {
    return undefined;
  }
  const digits = /^\d+$/.test(passcode);
  if (type === "pin" && !(digits && [4, 6].includes(passcode.length))) {
    throw new UsageError(t("passcode.invalidPin"));
  }
  if (type === "numeric" && !digits) {
    throw new UsageError(t("passcode.invalidNumeric"));
  }
  if (!/^[\x20-\x7e]{1,64}$/.test(passcode)) {
    throw new UsageError(t("passcode.invalidAlphanumeric"));
  }
  return { passcode, type };
}

export async function askPasscode(
  context: { signal: AbortSignal },
  platform: PasscodePlatform = "ios",
) {
  const choices =
    platform === "android"
      ? [
          {
            name: t("passcode.androidPin"),
            short: t("passcode.androidPinDetail"),
            value: "pin",
          },
          {
            name: t("passcode.pattern"),
            short: t("passcode.patternDetail"),
            value: "pattern",
          },
          {
            name: t("passcode.password"),
            short: t("passcode.passwordDetail"),
            value: "password",
          },
          {
            name: t("passcode.none"),
            short: t("passcode.noneDetail"),
            value: "none",
          },
          { name: t("noThanks"), value: null },
        ]
      : [
          {
            name: t("passcode.pin"),
            short: t("passcode.pinDetail"),
            value: "pin",
          },
          {
            name: t("passcode.numeric"),
            short: t("passcode.numericDetail"),
            value: "numeric",
          },
          {
            name: t("passcode.alphanumeric"),
            short: t("passcode.alphanumericDetail"),
            value: "alphanumeric",
          },
          { name: t("noThanks"), value: null },
        ];
  const type = await select<string | null>(
    {
      message: t("passcode.setUp"),
      theme: selectTheme,
      choices,
    },
    { output: process.stderr, signal: context.signal },
  );
  if (type === null) {
    return undefined;
  }
  if (platform === "android" && type === "none") {
    return { passcode: "", type: "none" };
  }
  const options = (passcode: string) => ({ passcode, "passcode-type": type });
  return passcodeOf(
    options(
      await password(
        {
          message:
            platform === "android" && type === "pattern"
              ? t("passcode.patternPrompt")
              : t("passcode.prompt"),
          theme: { style: { maskedText: t("passcode.masked") } },
          validate(passcode) {
            if (passcode === "") {
              return t("passcode.empty");
            }
            try {
              passcodeOf(options(passcode), platform);
              return true;
            } catch (error) {
              return errorMessage(error);
            }
          },
        },
        { output: process.stderr, signal: context.signal },
      ),
    ),
    platform,
  );
}
