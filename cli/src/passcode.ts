import password from "@inquirer/password";
import select from "@inquirer/select";

import { UsageError } from "./client";
import { tr } from "./i18n";
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
      tr("invalid argument {value} for --passcode-type: it's {types}", {
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
    throw new UsageError(
      tr(
        "A pin passcode is 4 or 6 digits; a custom one needs --passcode-type numeric or alphanumeric",
      ),
    );
  }
  if (type === "numeric" && !digits) {
    throw new UsageError(tr("A numeric passcode is digits only."));
  }
  if (!/^[\x20-\x7e]{1,64}$/.test(passcode)) {
    throw new UsageError(
      tr("A passcode is up to 64 printable ASCII characters."),
    );
  }
  return { passcode, type };
}

export async function askPasscode(context: { signal: AbortSignal }) {
  const type = await select<string | null>(
    {
      message: tr(
        "Set up auto-unlock? Swipt can then unlock this device by itself.",
      ),
      theme: selectTheme,
      choices: [
        {
          name: tr("4 or 6 digit passcode"),
          short: tr("4 or 6 digits"),
          value: "pin",
        },
        {
          name: tr("Custom numeric passcode"),
          short: tr("numeric"),
          value: "numeric",
        },
        {
          name: tr("Custom alphanumeric passcode"),
          short: tr("alphanumeric"),
          value: "alphanumeric",
        },
        { name: tr("No Thanks"), value: null },
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
          message: tr("Device passcode"),
          theme: { style: { maskedText: tr("[input is masked]") } },
          validate(passcode) {
            try {
              passcodeOf(options(passcode));
              return passcode !== "" || tr("Enter the passcode.");
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
