# Testing

| Check | When | Needs |
| --- | --- | --- |
| `go test ./...` | every change (CI runs it on Linux, macOS and Windows) | Go 1.27.3 |
| `npm test` with `WIRECAT_FUNNEL_TEST_BINARY` | a change to the launcher (CI runs it against the linux-x64 build) | Node 20+ |
| `bin/live-check` | before a release that changes `tailscale.com`, the proxy, the login flow or the log switch | a Tailscale account, a browser |
| [logs-off measurement](https://github.com/WireCatLabs/cli-testing/tree/main/performance/funnel/logs-off) | after a `tailscale.com` update | Go, `ss` |

Go tests cannot check that Tailscale's log upload is off: `tsnet` starts no logger at all under `go test`. Only the two
checks on the built binary can.

## bin/live-check

One real Funnel check on a temporary Tailscale device. You sign in in the browser; the script never does.

```sh
/path/to/funnel-cli/bin/live-check
```

1. Builds the helper and starts a dummy MCP endpoint on `127.0.0.1`.
2. Starts the helper with `--ephemeral` (state in memory, the device leaves the tailnet when it stops), hostname
   `wirecat-check`, and `--funnel-only`, so only a connection through Funnel is answered.
3. Prints the Tailscale login link. If the tailnet asks for device approval or Funnel is not allowed yet, it prints
   that link too.
4. Once the helper runs, it resolves the address through public DNS (`dig @1.1.1.1`) and calls `/mcp` with
   `curl --resolve`. That keeps the request off the tailnet path a system Tailscale on the same computer would take.
5. Counts the helper's lookups of `log.tailscale.com`, stops the helper (the device logs out and is removed) and
   removes its temporary folder.

**PASS** means both: the dummy server answered `initialize` through Funnel from the public internet, and the helper
never looked up Tailscale's log server. Anything else is FAIL, and a log lookup also means the privacy texts are
wrong. On FAIL the script keeps its folder (`run.log`, `dummy.log`) and says where to remove the device by hand.

**Expect a slow first call.** The HTTPS certificate is issued on the first connection, and a new name can take a few
minutes to appear in public DNS. The script retries every 15 s for 10 minutes. On 2026-11-05 the first three calls
failed on TLS and a timeout while the certificate was issued, then the fourth passed.

**Results** (totals only, never the tailnet name) go to cli-testing,
[performance/funnel](https://github.com/WireCatLabs/cli-testing/tree/main/performance/funnel).
