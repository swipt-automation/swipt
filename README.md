<p align="center">
  <img src="Swipt/Resources/AppIcon.icon/Assets/scroll.svg" alt="" width="160">
</p>

<h1 align="center">Swipt</h1>

<p align="center">The universal automation platform for iOS and Android.</p>

## Requirements

- iOS 27 (First-tier support)
- iOS 17.4–26 (I do not have actual devices for testing)
- Android 14 or later on arm64, with Shizuku or root

## Architecture

- Swipt SDK: A TypeScript SDK built upon CoreDevice, Accessibility services, and other developer services on iOS, and upon system services reached through Shizuku or root on Android.
- Swipt API (MCP-compatible): Allows you to run a JS REPL based on the Swipt SDK directly, enabling you to control an iPhone, iPad, or Android device, manage and debug Swiplets.
- Swipt CLI: A client for the Swipt API that offers a more comprehensive set of features than MCP.
- Swiplet: Apps that run within Swipt, built using the Swipt SDK and SwiftUI or Jetpack Compose.

## Documentation

https://swipt.io

## LICENSE

```text
Copyright (C) 2026 by nekohasekai <i@sekai.icu>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.

In addition, no derivative work may use the name or imply association
with this application without prior consent.
```
