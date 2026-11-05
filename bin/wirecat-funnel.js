#!/usr/bin/env node
import { spawnSync } from "node:child_process"
import { createRequire } from "node:module"

const require = createRequire(import.meta.url)
const target = `${process.platform}-${process.arch}`
const exe = process.platform === "win32" ? "wirecat-funnel.exe" : "wirecat-funnel"

const resolveBinary = () => {
  // Tests point this at a fresh build; nobody else needs it.
  if (process.env.WIRECAT_FUNNEL_BINARY) return process.env.WIRECAT_FUNNEL_BINARY
  try {
    return require.resolve(`@wirecat/funnel-${target}/bin/${exe}`)
  } catch {
    return null
  }
}

const binary = resolveBinary()
if (!binary) {
  console.error(
    `wirecat-funnel: no build for ${target}. npm installs @wirecat/funnel-${target} on its own; ` +
      "reinstall without --no-optional, or check that your system is one of darwin, linux, win32 on x64 or arm64.",
  )
  process.exit(1)
}

const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" })
if (result.error) {
  console.error(`wirecat-funnel: could not run ${binary}: ${result.error.message}`)
  process.exit(1)
}
process.exit(result.status ?? 1)
