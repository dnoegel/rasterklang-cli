import { readFileSync } from "node:fs";

const checks = [
  {
    file: "Makefile",
    text: "$(MAKE) staticcheck",
    message: "make check must run the staticcheck target",
  },
  {
    file: "Makefile",
    text: "staticcheck ./...",
    message: "Makefile must define a staticcheck target for all packages",
  },
  {
    file: ".github/workflows/binaries.yml",
    text: "honnef.co/go/tools/cmd/staticcheck@",
    message: "binary CI must install staticcheck before make check",
  },
  {
    file: ".github/workflows/release.yml",
    text: "honnef.co/go/tools/cmd/staticcheck@",
    message: "release CI must install staticcheck before make check",
  },
  {
    file: "docs/release.md",
    text: "staticcheck",
    message: "release docs must document the staticcheck release gate",
  },
  {
    file: "CONTRIBUTING.md",
    text: "staticcheck",
    message: "CONTRIBUTING must mention staticcheck for contributors",
  },
];

for (const check of checks) {
  const content = readFileSync(check.file, "utf8");
  if (!content.includes(check.text)) {
    console.error(`${check.file}: ${check.message}`);
    process.exit(1);
  }
}

console.log("Staticcheck release contract is present.");
