import { readFileSync } from "node:fs";

const checks = [
  {
    file: "Makefile",
    text: "race:",
    message: "Makefile must define a race target",
  },
  {
    file: "Makefile",
    text: "go test -race ./...",
    message: "race target must run all Go tests with the race detector",
  },
  {
    file: ".github/workflows/binaries.yml",
    text: "make race",
    message: "binary CI must run the race detector",
  },
  {
    file: ".github/workflows/release.yml",
    text: "make race",
    message: "release CI must run the race detector before publishing",
  },
  {
    file: "docs/release.md",
    text: "make race",
    message: "release docs must document the race detector gate",
  },
  {
    file: "CONTRIBUTING.md",
    text: "make race",
    message: "CONTRIBUTING must mention the race detector gate",
  },
];

for (const check of checks) {
  const content = readFileSync(check.file, "utf8");
  if (!content.includes(check.text)) {
    console.error(`${check.file}: ${check.message}`);
    process.exit(1);
  }
}

console.log("Race-detector release contract is present.");
