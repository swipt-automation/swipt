const [encodedMessages, id, mode, unlock, launchError, promptMode] = args;
const autoAllow = promptMode !== "no-prompts";
const messages = JSON.parse(encodedMessages);
const filled = (message, value) => message.replace(/\{\w+\}/, () => value);
const name = "Swipt";
const settings = "com.apple.Preferences";
const springboard = "com.apple.springboard";
const developerPattern =
  /(?:Apple Development|iPhone Developer): .+?\([A-Z0-9]{10}\)/;

const sheet = (timeoutMs) =>
  accessibility.find(
    { app: springboard, role: "sheet", label: /./ },
    { timeoutMs, poll: true },
  );

const inSettings = (selector, timeoutMs) =>
  accessibility.find({ app: settings, ...selector }, { timeoutMs, poll: true });

const escaped = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

async function tap(element) {
  try {
    await accessibility.tap(element, { timeoutMs: 0 });
  } catch (error) {
    if (error.name !== "TimeoutError") {
      throw error;
    }
  }
}

async function dismiss(alert, index) {
  const until = Date.now() + 10_000;
  let absent = 0;
  do {
    const shown = await sheet();
    if (shown === null) {
      absent += 1;
      if (absent === 2) {
        return;
      }
      continue;
    }
    if (shown.label !== alert.label) {
      return;
    }
    absent = 0;
    await tap(shown.findAll({ role: "button" })[index]);
  } while (Date.now() < until);
  throw new Error(`the alert stays: ${alert.label}`);
}

async function answerPrompt() {
  if (!autoAllow) {
    return false;
  }
  const alert = await sheet();
  const buttons = alert?.findAll({ role: "button" }) ?? [];
  if (alert?.label?.includes(name) && [2, 3].includes(buttons.length)) {
    // The location alert shows an icon and lists Allow Once first; the cellular
    // data alert of China mainland devices shows none and lists Wi-Fi and
    // cellular first (iOS 27.2 24B115). The background location upgrade alert
    // has 2 buttons and lists Change to Always Allow second (iOS 27.3 24C5037i).
    const location = alert.findAll({ role: "image" }).length > 0;
    await dismiss(alert, buttons.length === 2 || location ? 1 : 0);
  }
  return alert !== null;
}

async function untrustedDeveloper() {
  const until = Date.now() + 5_000;
  do {
    const alert = await sheet(Math.max(until - Date.now(), 0));
    const buttons = alert?.findAll({ role: "button" }) ?? [];
    if (buttons.length === 1) {
      const developer = alert
        .findAll({ role: "text" })
        .map((text) => developerPattern.exec(text.label ?? "")?.[0])
        .find((found) => found !== undefined);
      await dismiss(alert, 0);
      if (developer === undefined) {
        throw new Error(`the alert names no developer: ${alert.label}`);
      }
      return developer;
    }
  } while (Date.now() < until);
  throw new Error(launchError);
}

function developerRows(list, developer) {
  if (list.findAll({ role: "button", identifier: "VPN" }).length === 0) {
    return [];
  }
  const elsewhere = new Set(
    list
      .findAll({
        role: "button",
        within: {
          identifier: "com.apple.settings.sidebar.collectionView",
        },
      })
      .concat(
        list.findAll({
          role: "button",
          within: { role: /^(navigationBar|searchField)$/ },
        }),
      )
      .map((row) => row.identifier),
  );
  return list
    .findAll({ role: "button" })
    .filter(
      (row) =>
        row.identifier !== undefined &&
        row.identifier !== "BackButton" &&
        row.identifier !== "VPN" &&
        !row.identifier.startsWith("com.apple.settings.") &&
        row.identifier !== row.label &&
        !elsewhere.has(row.identifier),
    )
    .sort(
      (a, b) =>
        developer.includes(b.identifier) - developer.includes(a.identifier),
    );
}

async function openDeviceManagement(developer) {
  await device.launchApplication(settings, {
    url: "prefs:root=General&path=ManagedConfigurationList",
  });
  const until = Date.now() + 10_000;
  do {
    const list = await accessibility.snapshot({ scope: "frontApp" });
    const rows = developerRows(list, developer);
    if (rows.length > 0) {
      return rows;
    }
    await answerPrompt();
  } while (Date.now() < until);
  throw new Error(`Settings lists no app from ${developer}`);
}

async function allowDeveloper(row) {
  const control = {
    role: "text",
    label: new RegExp(
      `.${escaped(row.identifier)}|${escaped(row.identifier)}.`,
    ),
  };
  if ((await inSettings(control, 1_500)) === null) {
    return false;
  }
  const until = Date.now() + 5_000;
  do {
    const shown = await inSettings(control);
    if (shown !== null) {
      await input.tap(shown.point);
    }
    const alert = await sheet();
    const buttons = alert?.findAll({ role: "button" }) ?? [];
    if (buttons.length === 2) {
      await dismiss(alert, 0);
      while (
        (await inSettings(control)) !== null &&
        Date.now() < until + 10_000
      );
      return true;
    }
  } while (Date.now() < until);
  throw new Error("Settings did not ask to allow the developer");
}

async function trust(developer) {
  console.debug(filled(messages.trusting, developer));
  const page = { label: new RegExp(escaped(developer)) };
  for (const row of await openDeviceManagement(developer)) {
    const until = Date.now() + 3_000;
    let opened = null;
    do {
      await tap(row);
      opened = await inSettings(page);
    } while (opened === null && Date.now() < until);
    if (opened !== null && (await allowDeveloper(row))) {
      return;
    }
    const button = await inSettings({ identifier: "BackButton" }, 3_000);
    if (button === null) {
      throw new Error("Settings shows no back button");
    }
    await tap(button);
  }
  throw new Error(`Settings lists no app from ${developer}`);
}

async function waitForUnlock() {
  for await (const locked of device.locked.values()) {
    if (!locked) {
      return;
    }
    console.debug(messages.unlockToContinue);
  }
}

async function allowLocation() {
  await device.launchApplication(settings, {
    url: `app-prefs:${id}`,
    restart: true,
  });
  const until = Date.now() + 10_000;
  let row = null;
  do {
    await answerPrompt();
    row = await inSettings({ role: "button", identifier: id }, 500);
  } while (row === null && Date.now() < until);
  if (row === null) {
    await device.launchApplication(id);
    return;
  }
  let option = null;
  do {
    await tap(row);
    option = await inSettings(
      { identifier: "LOCATION_SERVICES_AUTH_ALWAYS" },
      1_000,
    );
  } while (option === null && Date.now() < until);
  if (option !== null && !option.selected && !option.disabled) {
    do {
      await input.tap(option.point);
      option = await inSettings(
        { identifier: "LOCATION_SERVICES_AUTH_ALWAYS" },
        1_000,
      );
    } while (option !== null && !option.selected && Date.now() < until);
  }
  await device.launchApplication(id);
}

async function finishSetup() {
  const until = Date.now() + 60_000;
  let finished = null;
  do {
    try {
      if (!(await answerPrompt())) {
        finished = await accessibility.find({
          app: id,
          identifier: "setup-finished",
        });
      }
    } catch (error) {
      if (
        error.name !== "TimeoutError" &&
        !/stopped answering/.test(error.message)
      ) {
        throw error;
      }
    }
  } while (finished === null && Date.now() < until);
  if (finished === null) {
    throw new Error(`${name} did not finish setting up`);
  }
  if (finished.value !== "always") {
    await allowLocation();
  }
  console.log(filled(messages.running, name));
}

async function main() {
  switch (mode) {
    case "unlock":
      if (unlock === "passcode") {
        console.debug(messages.unlocking);
        await device.unlock();
      } else if (unlock === "wait") {
        await waitForUnlock();
      }
      await answerPrompt();
      return;
    case "trust": {
      const developer = await untrustedDeveloper();
      await trust(developer);
      console.log(filled(messages.trusted, developer));
      return;
    }
    case "prompts":
      await finishSetup();
      return;
  }
}

await main();
