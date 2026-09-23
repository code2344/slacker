# Slacker

Slacker is a keyboard-driven Slack terminal client built from Discordo's TUI
foundations. It authenticates as your existing Slack user through the same
browser session used by Slack's web client; it does not install a bot or
require a Slack Marketplace app.

The project is in an early backend-replacement phase. The current build can:

- open an isolated browser window for Slack's normal interactive sign-in;
- optionally discover signed-in Slack Desktop workspaces on macOS, Linux, and Windows;
- import the bound browser token and session cookie into the OS keyring;
- validate the session and discover Enterprise Grid API routing;
- call Slack's user and internal client APIs with rate-limit handling;
- list channels, private channels, DMs, and group DMs in the Discordo-style TUI;
- keep one active workspace and switch it from the CLI.

Message history, the live WebSocket, sending, threads, and image rendering are
the next implementation slices.

## Build

Slacker currently requires the Go version declared in [go.mod](go.mod).

```sh
go build .
```

## Set up a workspace

Run Slacker and it will open Slack's sign-in page in a browser when needed:

```sh
slacker workspace add
```

The browser window is a temporary profile controlled by Slacker. Complete any
email, Google, Apple, or workspace SSO steps in that window. Slacker validates
the resulting session before saving it to the operating system credential
manager. Chrome or Chromium is required for this flow.

If Slack Desktop contains more than one workspace, choose one explicitly:

```sh
slacker workspace add example
slacker workspace add T0123456789
slacker workspace add --all
```

Importing Slack Desktop remains available as a fallback:

```sh
slacker workspace add --desktop
```

Workspace commands:

```sh
slacker workspace list
slacker workspace current
slacker workspace use example
slacker workspace remove example
slacker doctor
```

Open the active workspace with:

```sh
slacker
```

For protocol diagnostics, an arbitrary authenticated method can be called with:

```sh
slacker api auth.test
slacker api users.conversations limit=20
```

## Configuration

- Linux: `$XDG_CONFIG_HOME/slacker/config.toml` or
  `$HOME/.config/slacker/config.toml`
- macOS: `$HOME/Library/Application Support/slacker/config.toml`
- Windows: `%AppData%\slacker\config.toml`

Non-secret workspace metadata is stored beside the configuration. Browser
tokens and cookies are stored through the operating system credential manager.

## Unofficial protocol

Slacker interoperates with the internal browser protocol used by Slack's own
clients. That protocol can change without notice, so wire formats are isolated
from the TUI and covered by fixtures and transport tests.

Slacker is independent and is not affiliated with or endorsed by Slack
Technologies, LLC or Salesforce, Inc.

## Provenance and license

The terminal interface began as a clean fork of
[Discordo](https://github.com/ayn2op/discordo). Slack Desktop session discovery
is adapted from [slk](https://github.com/gammons/slk). See
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for attribution.

Slacker is licensed under GPL-3.0. See [LICENSE](LICENSE).
