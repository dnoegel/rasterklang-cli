import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";

const expectedOwner = "dnoegel";
const expectedRepo = "rasterklang-cli";
const expectedModule = "github.com/dnoegel/rasterklang-cli";
const firstReleaseVersion = "v0.1.0";

const modulePath = readModulePath();
const remoteUrl = readOriginRemote();
const remote = parseGitHubRemote(remoteUrl);
const tagCommit = readGitRef(`refs/tags/${firstReleaseVersion}^{commit}`);
const headCommit = readGitRef("HEAD");
const errors = [];

if (modulePath !== expectedModule) {
  errors.push(`go.mod module is ${modulePath || "<missing>"}, expected ${expectedModule}`);
}

if (!remote) {
  errors.push(`origin remote is not a recognized GitHub URL: ${remoteUrl || "<missing>"}`);
} else {
  const actual = `${remote.owner}/${remote.repo}`;
  const expected = `${expectedOwner}/${expectedRepo}`;
  if (actual !== expected) {
    errors.push(`origin remote is ${actual}, expected ${expected}`);
  }
}

if (tagCommit && headCommit && tagCommit !== headCommit) {
  errors.push(`${firstReleaseVersion} tag commit is ${tagCommit.slice(0, 12)}, expected current HEAD ${headCommit.slice(0, 12)}`);
}

if (errors.length > 0) {
  fail(
    [
      "release identity preflight failed",
      "",
      ...errors.map((error) => `- ${error}`),
      "",
      "Update the public repository name, origin remote, Go module path, README links, and release workflows before tagging.",
    ].join("\n"),
  );
}

console.log(`Release identity preflight passed for ${expectedOwner}/${expectedRepo} (${expectedModule}).`);

function readModulePath() {
  const goMod = readFileSync("go.mod", "utf8");
  return goMod.match(/^module\s+(\S+)/m)?.[1] ?? "";
}

function readOriginRemote() {
  const result = spawnSync("git", ["remote", "get-url", "origin"], {
    encoding: "utf8",
  });

  if (result.status !== 0) {
    return "";
  }

  return result.stdout.trim();
}

function readGitRef(ref) {
  const result = spawnSync("git", ["rev-parse", "--verify", ref], {
    encoding: "utf8",
  });

  if (result.status !== 0) {
    return "";
  }

  return result.stdout.trim();
}

function parseGitHubRemote(value) {
  const normalized = value.replace(/\.git$/, "");
  const patterns = [
    /^git@github\.com:([^/]+)\/(.+)$/,
    /^https:\/\/github\.com\/([^/]+)\/(.+)$/,
    /^ssh:\/\/git@github\.com\/([^/]+)\/(.+)$/,
  ];

  for (const pattern of patterns) {
    const match = normalized.match(pattern);
    if (match) {
      return { owner: match[1], repo: match[2] };
    }
  }

  return null;
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
