import password from "@inquirer/password";
import select from "@inquirer/select";

import { UsageError } from "./client";
import { t } from "./i18n";
import { errorMessage, selectTheme } from "./output";

export const passcodeTypes = ["pin", "numeric", "alphanumeric"];

export interface Passcode {
  passcode: string;
  type: string;
}

export interface PasscodeOptions {
  passcode?: string;
  "passcode-type"?: string;
}

export function passcodeOf(options: PasscodeOptions): Passcode | undefined {
  const type = options["passcode-type"] ?? "pin";
  if (!passcodeTypes.includes(type)) {
    throw new UsageError(
      t("passcode.invalidType", {
        value: JSON.stringify(type),
        types: passcodeTypes.join(", "),
      }),
    );
  }
  const passcode = options.passcode;
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

export async function askPasscode(context: { signal: AbortSignal }) {
  const type = await select<string | null>(
    {
      message: t("passcode.setUp"),
      theme: selectTheme,
      choices: [
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
      ],
    },
    { output: process.stderr, signal: context.signal },
  );
  if (type === null) {
    return undefined;
  }
  const options = (passcode: string) => ({ passcode, "passcode-type": type });
  return passcodeOf(
    options(
      await password(
        {
          message: t("passcode.prompt"),
          theme: { style: { maskedText: t("passcode.masked") } },
          validate(passcode) {
            try {
              passcodeOf(options(passcode));
              return passcode !== "" || t("passcode.empty");
            } catch (error) {
              return errorMessage(error);
            }
          },
        },
        { output: process.stderr, signal: context.signal },
      ),
    ),
  );
}
