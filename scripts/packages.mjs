// One npm package per platform, named by Node's process.platform and process.arch, so the launcher finds its own with
// require.resolve and npm installs only the package whose os and cpu match. Adapted from whatsapp-cli's
// scripts/engine-packages.mjs.
//
//   node scripts/packages.mjs build [<os>-<arch> ...]   # all six when none is named → packages/funnel-*/
//   node scripts/packages.mjs check                     # optionalDependencies pin package.json's version
//   node scripts/packages.mjs notices [--check]         # regenerate THIRD_PARTY_NOTICES, or fail when it is stale
//
// Plain Node with no dependencies: the release job runs it before anything is installed.
import { execFileSync } from "node:child_process"
import { chmodSync, copyFileSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

const root = join(dirname(fileURLToPath(import.meta.url)), "..")
const manifest = JSON.parse(readFileSync(join(root, "package.json"), "utf8"))
const noticesPath = join(root, "THIRD_PARTY_NOTICES")
const MAIN = "./cmd/wirecat-funnel"

/** Node's name for each target, then Go's. */
const TARGETS = {
  "darwin-arm64": ["darwin", "arm64"],
  "darwin-x64": ["darwin", "amd64"],
  "linux-arm64": ["linux", "arm64"],
  "linux-x64": ["linux", "amd64"],
  "win32-arm64": ["windows", "arm64"],
  "win32-x64": ["windows", "amd64"],
}

const packageName = (target) => `${manifest.name}-${target}`
const packageDir = (target) => join(root, "packages", `funnel-${target}`)
const go = (args, env = {}) => execFileSync("go", args, { cwd: root, env: { ...process.env, ...env }, encoding: "utf8" })

const LICENCE_FILE = /^(licen[cs]e|copying|notice|patents)/i
const licenceTexts = (dir) =>
  readdirSync(dir)
    .filter((name) => LICENCE_FILE.test(name) && statSync(join(dir, name)).isFile())
    .sort()
    .map((name) => `--- ${name}\n\n${readFileSync(join(dir, name), "utf8").trimEnd()}\n`)

const linkedModules = () => {
  const modules = new Set()
  for (const [goos, goarch] of Object.values(TARGETS)) {
    const listed = go(["list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}}@{{.Version}}{{end}}{{end}}", MAIN], {
      GOOS: goos,
      GOARCH: goarch,
      CGO_ENABLED: "0",
    })
    for (const line of listed.split("\n")) if (line) modules.add(line)
  }
  return [...modules].sort()
}

const proxyEscape = (text) => text.replace(/[A-Z]/g, (letter) => `!${letter.toLowerCase()}`)
const standardHead = () => `The Go standard library and runtime, ${go(["env", "GOVERSION"]).trim()}`
const expectedHeads = () => [...linkedModules().map((module) => module.replace("@", " ")), standardHead()]
const SEPARATOR = `\n${"=".repeat(80)}\n\n`

const writtenHeads = () => {
  try {
    return readFileSync(noticesPath, "utf8")
      .split(SEPARATOR)
      .slice(1)
      .map((section) => section.split("\n", 1)[0])
  } catch {
    return []
  }
}

const assertNoticesCurrent = () => {
  const expected = expectedHeads()
  const written = new Set(writtenHeads())
  const missing = expected.filter((head) => !written.has(head))
  const extra = [...written].filter((head) => !expected.includes(head))
  if (!missing.length && !extra.length) return
  for (const head of missing) console.error(`THIRD_PARTY_NOTICES lacks ${head}`)
  for (const head of extra) console.error(`THIRD_PARTY_NOTICES still names ${head}`)
  console.error("run `node scripts/packages.mjs notices` and commit the result")
  process.exit(1)
}

const notices = () => {
  const sections = linkedModules().map((module) => {
    const { Path, Version, Dir } = JSON.parse(go(["mod", "download", "-json", module]))
    const proxy = `https://proxy.golang.org/${proxyEscape(Path)}/@v/${proxyEscape(Version)}.zip`
    return [`${Path} ${Version}`, `Source: ${proxy}`, "", ...licenceTexts(Dir)].join("\n")
  })
  const goroot = go(["env", "GOROOT"]).trim()
  sections.push([standardHead(), "Source: https://go.dev/dl/", "", ...licenceTexts(goroot)].join("\n"))
  const header = [
    "Third-party material in wirecat-funnel",
    "",
    "bin/wirecat-funnel is built from funnel-cli (Apache-2.0, see LICENSE) and statically links the Go modules below",
    "and the Go standard library, all unmodified. Each module's licence files follow as they ship in that module at",
    "the version listed, with where its source is.",
    "",
    "Generated; regenerate with `node scripts/packages.mjs notices`.",
  ].join("\n")
  writeFileSync(noticesPath, [header, ...sections].join(SEPARATOR))
  console.log(`${noticesPath} — ${sections.length} components`)
}

const readme = (target) =>
  [
    `# ${packageName(target)}`,
    "",
    `The ${target} build of [${manifest.name}](https://www.npmjs.com/package/${manifest.name}): the Go program`,
    "`bin/wirecat-funnel`. Install the main package, not this one; npm adds the build for your system on its own.",
    "",
    "Apache-2.0 (see LICENSE). THIRD_PARTY_NOTICES lists every Go module the binary links and its licence,",
    "Tailscale's BSD-3-Clause among them.",
    "",
  ].join("\n")

const build = (target) => {
  const [goos, goarch] = TARGETS[target] ?? []
  if (!goos) throw new Error(`unknown target ${target} — one of ${Object.keys(TARGETS).join(", ")}`)
  const [os, cpu] = target.split("-")
  const dir = packageDir(target)
  const binary = join(dir, "bin", `wirecat-funnel${goos === "windows" ? ".exe" : ""}`)
  rmSync(dir, { recursive: true, force: true })
  mkdirSync(dirname(binary), { recursive: true })
  execFileSync(
    "go",
    ["build", "-trimpath", "-ldflags", `-s -w -X main.version=${manifest.version}`, "-o", binary, MAIN],
    // Without cgo the binary is static: it runs on glibc and musl alike, and every target builds on one runner.
    { cwd: root, env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: "0" }, stdio: "inherit" },
  )
  chmodSync(binary, 0o755)
  copyFileSync(join(root, "LICENSE"), join(dir, "LICENSE"))
  copyFileSync(noticesPath, join(dir, "THIRD_PARTY_NOTICES"))
  writeFileSync(join(dir, "README.md"), readme(target))
  const platform = {
    name: packageName(target),
    version: manifest.version,
    description: `The ${target} build of ${manifest.name}`,
    license: manifest.license,
    author: manifest.author,
    // npm provenance refuses a package whose repository is not the one that built it.
    repository: manifest.repository,
    os: [os],
    cpu: [cpu],
    files: ["bin", "LICENSE", "README.md", "THIRD_PARTY_NOTICES"],
    publishConfig: { access: "public" },
  }
  writeFileSync(join(dir, "package.json"), `${JSON.stringify(platform, null, 2)}\n`)
  console.log(`${platform.name}@${platform.version} → ${dir}`)
}

const [command, ...args] = process.argv.slice(2)
if (command === "build") {
  assertNoticesCurrent()
  for (const target of args.length ? args : Object.keys(TARGETS)) build(target)
} else if (command === "check") {
  const pins = Object.keys(TARGETS).map((target) => [packageName(target), manifest.optionalDependencies?.[packageName(target)]])
  const stale = pins.filter(([, version]) => version !== manifest.version)
  for (const [name, version] of stale) console.error(`${name} is ${version ?? "missing"}, not ${manifest.version}`)
  if (stale.length) process.exit(1)
  console.log(`platform packages ${manifest.version}: all ${pins.length} optionalDependencies agree`)
} else if (command === "notices") {
  if (args.includes("--check")) {
    assertNoticesCurrent()
    console.log("THIRD_PARTY_NOTICES names every linked module and Go")
  } else notices()
} else {
  console.error("usage: node scripts/packages.mjs build [<os>-<arch> ...] | check | notices [--check]")
  process.exit(2)
}
