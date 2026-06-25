#!/usr/bin/env node
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

const args = parseArgs(process.argv.slice(2));
const version = required(args.version, "--version");
const normalizedVersion = version.replace(/^v/, "");
const distDir = resolve(args.dist || "dist");
const outDir = resolve(args.out || join(distDir, "package-manifests"));
const releaseBaseUrl = (args.releaseBaseUrl || "https://github.com/dnoegel/rasterklang-cli/releases/download").replace(/\/$/, "");
const homepage = "https://github.com/dnoegel/rasterklang-cli";

const artifacts = {
  linuxAmd64: readArtifact("rasterklang-linux-amd64.tar.gz"),
  linuxArm64: readArtifact("rasterklang-linux-arm64.tar.gz"),
  macosAmd64: readArtifact("rasterklang-macos-amd64.tar.gz"),
  macosArm64: readArtifact("rasterklang-macos-arm64.tar.gz"),
  windowsAmd64: readArtifact("rasterklang-windows-amd64.tar.gz"),
  windowsArm64: readArtifact("rasterklang-windows-arm64.tar.gz"),
};

write(join(outDir, "homebrew/rasterklang.rb"), renderHomebrewFormula());
write(join(outDir, "scoop/rasterklang.json"), `${JSON.stringify(renderScoopManifest(), null, 2)}\n`);

console.log(`Generated package manifests in ${outDir}`);

function renderHomebrewFormula() {
  return `class Rasterklang < Formula
  desc "Pure-Go SID engine and CLI for PSID/RSID tunes"
  homepage "${homepage}"
  version "${normalizedVersion}"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "${artifactUrl(artifacts.macosArm64.name)}"
      sha256 "${artifacts.macosArm64.sha256}"
    else
      url "${artifactUrl(artifacts.macosAmd64.name)}"
      sha256 "${artifacts.macosAmd64.sha256}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "${artifactUrl(artifacts.linuxArm64.name)}"
      sha256 "${artifacts.linuxArm64.sha256}"
    else
      url "${artifactUrl(artifacts.linuxAmd64.name)}"
      sha256 "${artifacts.linuxAmd64.sha256}"
    end
  end

  def install
    os = OS.mac? ? "macos" : "linux"
    arch = Hardware::CPU.arm? ? "arm64" : "amd64"
    artifact_dir = "rasterklang-#{os}-#{arch}"
    binary_name = artifact_dir
    bin.install "#{artifact_dir}/#{binary_name}" => "rasterklang"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/rasterklang --version")
  end
end
`;
}

function renderScoopManifest() {
  return {
    version: normalizedVersion,
    description: "Pure-Go SID engine and CLI for PSID/RSID tunes.",
    homepage,
    license: "MIT",
    architecture: {
      "64bit": {
        url: artifactUrl(artifacts.windowsAmd64.name),
        hash: artifacts.windowsAmd64.sha256,
        extract_dir: "rasterklang-windows-amd64",
      },
      arm64: {
        url: artifactUrl(artifacts.windowsArm64.name),
        hash: artifacts.windowsArm64.sha256,
        extract_dir: "rasterklang-windows-arm64",
      },
    },
    pre_install:
      "Get-ChildItem \"$dir\" -Filter 'rasterklang-windows-*.exe' | Select-Object -First 1 | Rename-Item -NewName 'rasterklang.exe'",
    bin: "rasterklang.exe",
    checkver: {
      github: "https://github.com/dnoegel/rasterklang-cli",
    },
    autoupdate: {
      architecture: {
        "64bit": {
          url: `${releaseBaseUrl}/v$version/rasterklang-windows-amd64.tar.gz`,
          extract_dir: "rasterklang-windows-amd64",
        },
        arm64: {
          url: `${releaseBaseUrl}/v$version/rasterklang-windows-arm64.tar.gz`,
          extract_dir: "rasterklang-windows-arm64",
        },
      },
    },
  };
}

function readArtifact(name) {
  const checksumPath = join(distDir, `${name}.sha256`);
  if (!existsSync(checksumPath)) {
    fail(`missing checksum file: ${checksumPath}`);
  }

  const checksum = readFileSync(checksumPath, "utf8").trim();
  const match = checksum.match(/^([a-f0-9]{64})\s+(.+)$/);
  if (!match) {
    fail(`invalid checksum format in ${checksumPath}`);
  }
  if (match[2] !== name) {
    fail(`checksum file ${checksumPath} references ${match[2]}, expected ${name}`);
  }

  return { name, sha256: match[1] };
}

function artifactUrl(name) {
  return `${releaseBaseUrl}/${version}/${name}`;
}

function write(path, content) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, content);
}

function parseArgs(argv) {
  const parsed = {};
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (!arg.startsWith("--")) {
      fail(`unexpected argument: ${arg}`);
    }
    const key = arg.slice(2).replace(/-([a-z])/g, (_, char) => char.toUpperCase());
    const value = argv[index + 1];
    if (!value || value.startsWith("--")) {
      fail(`${arg} requires a value`);
    }
    parsed[key] = value;
    index += 1;
  }
  return parsed;
}

function required(value, name) {
  if (!value) {
    fail(`${name} is required`);
  }
  return value;
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
