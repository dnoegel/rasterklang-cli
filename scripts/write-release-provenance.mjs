import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT_DIR = resolve(dirname(fileURLToPath(import.meta.url)), "..");

const args = new Map();
for (let i = 2; i < process.argv.length; i += 2) {
  const key = process.argv[i];
  const value = process.argv[i + 1];
  if (!key?.startsWith("--") || value === undefined) {
    throw new Error(`Invalid argument pair at ${key || "<empty>"}`);
  }
  args.set(key.slice(2), value);
}

const out = required("out");
const provenance = {
  schemaVersion: 1,
  name: required("name"),
  version: required("version"),
  commit: required("commit"),
  date: required("date"),
  sourceRepository: required("source-repository"),
  artifact: {
    kind: required("artifact-kind"),
    name: required("artifact-name"),
    targetOs: optional("target-os"),
    targetArch: optional("target-arch"),
  },
  build: {
    command: optional("build-command") || "make dist",
    sourceDirty: sourceDirty(),
    github: githubContext(),
  },
};

mkdirSync(dirname(out), { recursive: true });
writeFileSync(out, `${JSON.stringify(provenance, null, 2)}\n`);

function required(name) {
  const value = args.get(name);
  if (!value) throw new Error(`Missing required --${name}`);
  return value;
}

function optional(name) {
  return args.get(name) || undefined;
}

function sourceDirty() {
  if (process.env.SOURCE_DIRTY === "true") return true;
  if (process.env.SOURCE_DIRTY === "false") return false;
  try {
    return execFileSync("git", ["status", "--porcelain"], {
      cwd: ROOT_DIR,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim().length > 0;
  } catch {
    return undefined;
  }
}

function githubContext() {
  const context = {};
  for (const [envName, key] of [
    ["GITHUB_REPOSITORY", "repository"],
    ["GITHUB_REF_NAME", "refName"],
    ["GITHUB_SHA", "sha"],
    ["GITHUB_RUN_ID", "runId"],
    ["GITHUB_RUN_ATTEMPT", "runAttempt"],
  ]) {
    if (process.env[envName]) context[key] = process.env[envName];
  }
  return context;
}
