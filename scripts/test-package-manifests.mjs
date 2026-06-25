import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

const repoRoot = new URL("..", import.meta.url).pathname;
const script = join(repoRoot, "scripts/generate-package-manifests.mjs");
const releaseWorkflow = readFileSync(join(repoRoot, ".github/workflows/release.yml"), "utf8");
const packageWorkflow = readFileSync(join(repoRoot, ".github/workflows/package-channels.yml"), "utf8");
const tempRoot = mkdtempSync(join(tmpdir(), "rasterklang-packages-"));
const distDir = join(tempRoot, "dist");
const outDir = join(tempRoot, "package-manifests");

const version = "v0.1.0";
const releaseBaseUrl = "https://github.com/dnoegel/rasterklang-cli/releases/download";
const checksums = {
  "rasterklang-linux-amd64.tar.gz": "a".repeat(64),
  "rasterklang-linux-arm64.tar.gz": "b".repeat(64),
  "rasterklang-macos-amd64.tar.gz": "c".repeat(64),
  "rasterklang-macos-arm64.tar.gz": "d".repeat(64),
  "rasterklang-windows-amd64.tar.gz": "e".repeat(64),
  "rasterklang-windows-arm64.tar.gz": "f".repeat(64),
};

try {
  mkdirSync(distDir, { recursive: true });
  for (const [archive, checksum] of Object.entries(checksums)) {
    writeFileSync(join(distDir, archive), "");
    writeFileSync(join(distDir, `${archive}.sha256`), `${checksum}  ${archive}\n`);
  }

  const generated = spawnSync(
    process.execPath,
    [
      script,
      "--version",
      version,
      "--dist",
      distDir,
      "--out",
      outDir,
      "--release-base-url",
      releaseBaseUrl,
    ],
    { cwd: repoRoot, encoding: "utf8" },
  );
  assert.equal(generated.status, 0, generated.stderr || generated.stdout);

  const formula = readFileSync(join(outDir, "homebrew/rasterklang.rb"), "utf8");
  assert.match(formula, /class Rasterklang < Formula/);
  assert.match(formula, /version "0\.1\.0"/);
  assert.match(formula, /license "MIT"/);
  assert.match(formula, /on_macos do/);
  assert.match(formula, /on_linux do/);
  assert.match(formula, new RegExp(`${releaseBaseUrl}/${version}/rasterklang-macos-arm64\\.tar\\.gz`));
  assert.match(formula, new RegExp(`${releaseBaseUrl}/${version}/rasterklang-linux-amd64\\.tar\\.gz`));
  assert.match(formula, /sha256 "d{64}"/);
  assert.match(formula, /sha256 "a{64}"/);
  assert.match(formula, /bin\.install "\#\{artifact_dir\}\/\#\{binary_name\}" => "rasterklang"/);

  const scoop = JSON.parse(readFileSync(join(outDir, "scoop/rasterklang.json"), "utf8"));
  assert.equal(scoop.version, "0.1.0");
  assert.equal(scoop.homepage, "https://github.com/dnoegel/rasterklang-cli");
  assert.equal(scoop.license, "MIT");
  assert.equal(
    scoop.architecture["64bit"].url,
    `${releaseBaseUrl}/${version}/rasterklang-windows-amd64.tar.gz`,
  );
  assert.equal(scoop.architecture["64bit"].hash, checksums["rasterklang-windows-amd64.tar.gz"]);
  assert.equal(
    scoop.architecture.arm64.url,
    `${releaseBaseUrl}/${version}/rasterklang-windows-arm64.tar.gz`,
  );
  assert.equal(scoop.architecture.arm64.hash, checksums["rasterklang-windows-arm64.tar.gz"]);
  assert.equal(scoop.bin, "rasterklang.exe");
  assert.match(scoop.pre_install, /rasterklang-windows-\*\.exe/);
  assert.match(scoop.checkver.github, /dnoegel\/rasterklang/);

  assert.match(releaseWorkflow, /dist\/package-manifests\/homebrew\/rasterklang\.rb/);
  assert.match(releaseWorkflow, /dist\/package-manifests\/scoop\/rasterklang\.json/);

  for (const phrase of [
    "name: Package Channels",
    "workflow_dispatch:",
    "release_tag",
    "homebrew_tap_repo",
    "scoop_bucket_repo",
    "publish_homebrew",
    "publish_scoop",
    "dry_run",
    "dist/package-manifests/homebrew/rasterklang.rb",
    "dist/package-manifests/scoop/rasterklang.json",
    "gh release download",
    "dnoegel/rasterklang-cli",
    "brew audit --strict --online",
    "scoop install",
    "create-pull-request",
  ]) {
    assert.match(packageWorkflow, new RegExp(escapeRegExp(phrase)), `package workflow should include ${phrase}`);
  }

  rmSync(join(distDir, "rasterklang-linux-amd64.tar.gz.sha256"));
  const failed = spawnSync(
    process.execPath,
    [script, "--version", version, "--dist", distDir, "--out", outDir],
    { cwd: repoRoot, encoding: "utf8" },
  );
  assert.notEqual(failed.status, 0);
  assert.match(failed.stderr, /missing checksum file.*rasterklang-linux-amd64\.tar\.gz\.sha256/);
} finally {
  rmSync(tempRoot, { recursive: true, force: true });
}

console.log("Package manifest generator test passed.");

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
