import assert from "node:assert/strict"
import { spawnSync } from "node:child_process"
import { mkdtempSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { test } from "node:test"
import { fileURLToPath } from "node:url"

const launcher = fileURLToPath(new URL("../bin/wirecat-funnel.js", import.meta.url))
const built = process.env.WIRECAT_FUNNEL_TEST_BINARY

const launch = (args, env = {}) =>
  spawnSync(process.execPath, [launcher, ...args], {
    encoding: "utf8",
    env: { ...process.env, WIRECAT_FUNNEL_STATE_DIR: mkdtempSync(join(tmpdir(), "funnel-")), ...env },
  })

test("without a platform package it names the missing one and fails", () => {
  const result = launch(["--version"], { WIRECAT_FUNNEL_BINARY: "" })
  assert.equal(result.status, 1)
  assert.match(result.stderr, new RegExp(`@wirecat/funnel-${process.platform}-${process.arch}`))
})

test("it passes arguments, output and the exit code through", { skip: !built && "no WIRECAT_FUNNEL_TEST_BINARY" }, () => {
  const ok = launch(["status", "--json"], { WIRECAT_FUNNEL_BINARY: built })
  assert.equal(ok.status, 0)
  assert.equal(JSON.parse(ok.stdout).state, "stopped")
  const bad = launch(["route", "add", "nope", "1", "--json"], { WIRECAT_FUNNEL_BINARY: built })
  assert.equal(bad.status, 1)
  assert.ok(JSON.parse(bad.stdout).error)
})
