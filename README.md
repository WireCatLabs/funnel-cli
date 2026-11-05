# @wirecat/funnel

`wirecat-funnel` publishes the MCP servers on your computer at one public HTTPS address through
[Tailscale Funnel](https://tailscale.com/kb/1223/funnel). It needs no administrator password and no Tailscale app:
it is its own small Tailscale device, built on Tailscale's [`tsnet`](https://tailscale.com/kb/1244/tsnet) library,
with its state in one folder of yours. WireCat Desktop installs and drives it; you can also use it from a terminal.

```sh
npm install --global @wirecat/funnel
wirecat-funnel start            # prints a Tailscale sign-in link the first time
wirecat-funnel route add /tg 8765
wirecat-funnel status           # running: https://wirecat.<your-tailnet>.ts.net
wirecat-funnel stop
```

## Commands

| Command | What it does |
| --- | --- |
| `start [--hostname <name>]` | starts the helper in the background, waits until it runs or needs you |
| `stop` | stops it |
| `status` | where it is: `stopped`, `starting`, `needs-login`, `needs-approval`, `needs-funnel`, `running` or `error` |
| `route add <path> <port>` | sends `https://<address><path>…` to `http://127.0.0.1:<port><path>…` |
| `route remove <path>` | removes that route |
| `route list` | lists the routes |
| `run [--hostname <name>] [--ephemeral]` | the helper in the foreground |

Add `--json` to any command for one JSON object:

```json
{
  "state": "needs-login",
  "pid": 41231,
  "url": null,
  "action_url": "https://login.tailscale.com/a/…",
  "error": null,
  "routes": [{ "path": "/tg", "port": 8765 }],
  "dir": "/home/you/.local/share/wirecat-funnel",
  "version": "0.1.0"
}
```

`action_url` is the page to open next: sign in (`needs-login`), approve the device (`needs-approval`) or allow
Funnel (`needs-funnel`). A failed command prints `{"error": "…"}` and exits 1.

## Privacy

- Tailscale's log upload and support data are always off. Tailscale's own log settings file and empty log buffers
  stay in the state folder and never leave your computer.
- Tailscale still sees this device in your tailnet (its name, system and versions) and the metadata of each
  connection (addresses, times, sizes). It cannot read the content: TLS ends on your computer.
- The address `https://<name>.<tailnet>.ts.net` gets a public certificate, so it appears in public certificate logs.
  The default name is `wirecat`, so it does not carry your computer's name.

## State

`$WIRECAT_FUNNEL_STATE_DIR`, else `~/.local/share/wirecat-funnel` (`$XDG_DATA_HOME` if set), or
`%LOCALAPPDATA%\wirecat-funnel\Data` on Windows. Folder mode 700. It holds the device's keys: treat it as a secret.

## Development

```sh
go test ./...
node scripts/packages.mjs build linux-x64
WIRECAT_FUNNEL_TEST_BINARY=packages/funnel-linux-x64/bin/wirecat-funnel npm test
bin/live-check      # one real Funnel check on a temporary device; you sign in in the browser
```

`bin/live-check` passed end to end on 2026-11-05: the call through public DNS reached the server, and the helper never
looked up Tailscale's log server. When to run it, what PASS means and why the first calls fail while the certificate
is issued: [docs/TESTING.md](docs/TESTING.md). Releasing: [docs/RELEASING.md](docs/RELEASING.md).

Apache-2.0. `THIRD_PARTY_NOTICES` lists every Go module the binary links, Tailscale's BSD-3-Clause among them.
