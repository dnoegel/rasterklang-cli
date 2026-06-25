#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { basename, dirname, join, relative, resolve } from "node:path";

const args = parseArgs(process.argv.slice(2));
const projectRoot = resolve(args.project || ".");
const outPath = args.out ? resolve(args.out) : null;
const failOnUnknown = Boolean(args.failOnUnknown);

const sections = [];
const summaries = new Map();
const unknowns = [];

if (existsSync(join(projectRoot, "go.mod"))) {
  const rows = collectGoModules(projectRoot);
  if (rows.length > 0) {
    sections.push(renderTable("Go Modules", ["Type", "Module", "Version", "License", "Evidence"], rows));
    recordRows(rows);
  }
}

if (existsSync(join(projectRoot, "package-lock.json"))) {
  const rows = collectNpmPackages(projectRoot);
  if (rows.length > 0) {
    sections.push(renderTable("npm Packages", ["Scope", "Package", "Version", "License", "Evidence"], rows));
    recordRows(rows);
  }
}

if (sections.length === 0) {
  fail(`No supported dependency manifests found in ${projectRoot}`);
}

const report = [
  "# Dependency License Report",
  "",
  `Project: \`${basename(projectRoot)}\``,
  "",
  "Generated from local dependency manifests. Review this report from the exact tagged source state before publishing release artifacts.",
  "",
  ...sections,
  renderSummary(),
  "## Review Notes",
  "",
  "- This report is generated from metadata and local license files. It is not legal advice.",
  "- Include this report or an equivalent notice bundle with release artifacts.",
  "- Resolve every `UNKNOWN` entry before publishing a public release.",
  "",
].join("\n");

if (outPath) {
  mkdirSync(dirname(outPath), { recursive: true });
  writeFileSync(outPath, report);
} else {
  process.stdout.write(report);
}

if (failOnUnknown && unknowns.length > 0) {
  fail(`Unknown licenses found:\n${unknowns.map((item) => `- ${item}`).join("\n")}`);
}

function collectGoModules(root) {
  const result = spawnSync("go", ["list", "-m", "-json", "all"], {
    cwd: root,
    env: { ...process.env, GOWORK: "off" },
    encoding: "utf8",
  });
  if (result.status !== 0) {
    return collectGoModulesFromManifests(root);
  }

  return parseGoListJson(result.stdout)
    .map((module) => ensureGoModuleDir(root, module))
    .map((module) => {
      const replacement = module.Replace;
      const active = replacement || module;
      const evidenceDir = active.Dir || module.Dir || "";
      const detected = detectLicense(evidenceDir);
      const moduleLabel = replacement
        ? `${module.Path} => ${replacement.Path || replacement.Dir}`
        : module.Path;
      const version = replacement?.Version || module.Version || "(main)";
      const type = module.Main ? "main" : replacement ? "replace" : "module";
      const evidence = detected.file ? relativePath(root, detected.file) : "not found";
      return [type, moduleLabel, version, detected.license, evidence];
    })
    .sort((a, b) => a[1].localeCompare(b[1]));
}

function ensureGoModuleDir(root, module) {
  const active = module.Replace || module;
  if (active.Dir || module.Main || !active.Path || !active.Version) {
    return module;
  }

  const result = spawnSync("go", ["mod", "download", "-json", `${active.Path}@${active.Version}`], {
    cwd: root,
    env: { ...process.env, GOWORK: "off" },
    encoding: "utf8",
  });
  if (result.status !== 0) {
    return module;
  }

  try {
    const downloaded = JSON.parse(result.stdout);
    if (!downloaded.Dir) {
      return module;
    }
    if (module.Replace) {
      return { ...module, Replace: { ...module.Replace, Dir: downloaded.Dir } };
    }
    return { ...module, Dir: downloaded.Dir };
  } catch {
    return module;
  }
}

function collectGoModulesFromManifests(root) {
  const goModPath = join(root, "go.mod");
  const parsed = parseGoMod(readFileSync(goModPath, "utf8"));
  const modules = new Map();
  modules.set(`${parsed.modulePath}@`, {
    type: "main",
    path: parsed.modulePath,
    version: "(main)",
  });

  for (const required of parsed.requires) {
    modules.set(`${required.path}@${required.version}`, required);
  }

  const siblingModules = findSiblingModules(root);
  const modCache = getGoModCache(root);

  return [...modules.values()]
    .map((module) => {
      const dir = resolveGoModuleDir(root, module, siblingModules, modCache);
      const detected = detectLicense(dir);
      const evidence = detected.file ? relativePath(root, detected.file) : "not found";
      return [module.type, module.path, module.version, detected.license, evidence];
    })
    .sort((a, b) => a[1].localeCompare(b[1]) || a[2].localeCompare(b[2]));
}

function parseGoMod(content) {
  const moduleMatch = content.match(/^module\s+(\S+)/m);
  if (!moduleMatch) {
    fail("go.mod is missing a module declaration");
  }

  const requires = [];
  let inRequireBlock = false;
  for (const rawLine of content.split(/\r?\n/)) {
    const trimmed = rawLine.trim();
    if (trimmed === "" || trimmed.startsWith("//")) continue;
    if (trimmed === "require (") {
      inRequireBlock = true;
      continue;
    }
    if (inRequireBlock && trimmed === ")") {
      inRequireBlock = false;
      continue;
    }

    let entry = "";
    if (inRequireBlock) {
      entry = trimmed;
    } else if (trimmed.startsWith("require ")) {
      entry = trimmed.slice("require ".length).trim();
    } else {
      continue;
    }

    const indirect = entry.includes("// indirect");
    const parts = entry.replace(/\/\/.*$/, "").trim().split(/\s+/);
    if (parts.length >= 2) {
      requires.push({
        type: indirect ? "indirect" : "module",
        path: parts[0],
        version: parts[1],
      });
    }
  }

  return { modulePath: moduleMatch[1], requires };
}

function findSiblingModules(root) {
  const modules = new Map();
  const parent = dirname(root);
  for (const entry of readdirSync(parent, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const goMod = join(parent, entry.name, "go.mod");
    if (!existsSync(goMod)) continue;
    const match = readFileSync(goMod, "utf8").match(/^module\s+(\S+)/m);
    if (match) {
      modules.set(match[1], join(parent, entry.name));
    }
  }
  return modules;
}

function getGoModCache(root) {
  const result = spawnSync("go", ["env", "GOMODCACHE"], {
    cwd: root,
    env: { ...process.env, GOWORK: "off" },
    encoding: "utf8",
  });
  if (result.status !== 0) {
    return "";
  }
  return result.stdout.trim();
}

function resolveGoModuleDir(root, module, siblingModules, modCache) {
  if (module.version === "(main)") return root;
  if (siblingModules.has(module.path)) return siblingModules.get(module.path);
  if (modCache) {
    const cacheDir = join(modCache, `${escapeGoModulePath(module.path)}@${escapeGoModulePath(module.version)}`);
    if (existsSync(cacheDir)) return cacheDir;
  }
  return downloadGoModuleDir(root, module);
}

function downloadGoModuleDir(root, module) {
  const result = spawnSync("go", ["mod", "download", "-json", `${module.path}@${module.version}`], {
    cwd: root,
    env: { ...process.env, GOWORK: "off" },
    encoding: "utf8",
  });
  if (result.status !== 0) {
    return "";
  }

  try {
    return JSON.parse(result.stdout).Dir || "";
  } catch {
    return "";
  }
}

function escapeGoModulePath(value) {
  return value.replace(/[A-Z]/g, (char) => `!${char.toLowerCase()}`);
}

function collectNpmPackages(root) {
  const lock = JSON.parse(readFileSync(join(root, "package-lock.json"), "utf8"));
  const rootPackage = lock.packages?.[""] || {};
  const directDeps = new Set([
    ...Object.keys(rootPackage.dependencies || {}),
    ...Object.keys(rootPackage.optionalDependencies || {}),
  ]);
  const devDeps = new Set(Object.keys(rootPackage.devDependencies || {}));

  return Object.entries(lock.packages || {})
    .filter(([path]) => path.startsWith("node_modules/"))
    .map(([path, entry]) => {
      const name = path.replace(/^node_modules\//, "");
      const scope = directDeps.has(name) ? "runtime" : devDeps.has(name) || entry.dev ? "dev" : "transitive";
      const license = normalizeLicense(entry.license || "UNKNOWN");
      const version = entry.version || "";
      return [scope, name, version, license, "package-lock.json"];
    })
    .sort((a, b) => a[1].localeCompare(b[1]));
}

function detectLicense(dir) {
  if (!dir || !existsSync(dir)) {
    return { license: "UNKNOWN", file: "" };
  }
  const files = readdirSync(dir, { withFileTypes: true })
    .filter((entry) => entry.isFile())
    .map((entry) => entry.name)
    .filter((name) => /^(license|licence|copying|notice|patents)(\.|$)/i.test(name))
    .sort((a, b) => a.localeCompare(b));

  const licenseFiles = files.filter((name) => /^(license|licence|copying)(\.|$)/i.test(name));
  const candidates = licenseFiles.length > 0 ? licenseFiles : files;
  for (const file of candidates) {
    const path = join(dir, file);
    const content = readFileSync(path, "utf8");
    const license = detectLicenseText(content);
    if (license !== "UNKNOWN") {
      return { license, file: path };
    }
  }
  return { license: "UNKNOWN", file: candidates[0] ? join(dir, candidates[0]) : "" };
}

function detectLicenseText(content) {
  const text = content.toLowerCase();
  if (text.includes("apache license") && text.includes("version 2.0")) return "Apache-2.0";
  if (text.includes("mit license") || text.includes("permission is hereby granted, free of charge")) return "MIT";
  if (text.includes("mozilla public license") && text.includes("version 2.0")) return "MPL-2.0";
  if (text.includes("isc license")) return "ISC";
  if (text.includes("creative commons attribution 4.0")) return "CC-BY-4.0";
  if (text.includes("zero-clause bsd") || text.includes("0bsd")) return "0BSD";
  if (text.includes("sqlite is public domain") || text.includes("public domain by the authors")) return "Public-Domain";
  if (text.includes("redistribution and use in source and binary forms")) {
    if (text.includes("neither the name")) return "BSD-3-Clause";
    return "BSD-2-Clause";
  }
  return "UNKNOWN";
}

function parseGoListJson(output) {
  const modules = [];
  let depth = 0;
  let start = -1;
  for (let index = 0; index < output.length; index += 1) {
    const char = output[index];
    if (char === "{") {
      if (depth === 0) start = index;
      depth += 1;
    } else if (char === "}") {
      depth -= 1;
      if (depth === 0 && start !== -1) {
        modules.push(JSON.parse(output.slice(start, index + 1)));
        start = -1;
      }
    }
  }
  return modules;
}

function renderTable(title, headers, rows) {
  return [
    `## ${title}`,
    "",
    `| ${headers.join(" | ")} |`,
    `| ${headers.map(() => "---").join(" | ")} |`,
    ...rows.map((row) => `| ${row.map(markdownCell).join(" | ")} |`),
    "",
  ].join("\n");
}

function renderSummary() {
  const rows = [...summaries.entries()]
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([license, count]) => [license, String(count)]);
  return renderTable("License Summary", ["License", "Count"], rows);
}

function recordRows(rows) {
  for (const row of rows) {
    const license = normalizeLicense(row[row.length - 2]);
    summaries.set(license, (summaries.get(license) || 0) + 1);
    if (license === "UNKNOWN") {
      unknowns.push(row.slice(0, -1).join(" "));
    }
  }
}

function markdownCell(value) {
  return String(value || "")
    .replace(/\|/g, "\\|")
    .replace(/\n/g, " ");
}

function normalizeLicense(value) {
  return String(value || "UNKNOWN").trim() || "UNKNOWN";
}

function relativePath(root, path) {
  const rel = relative(root, path);
  return rel.startsWith("..") ? path : rel || ".";
}

function parseArgs(argv) {
  const parsed = {};
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === "--project") {
      parsed.project = requireValue(argv, ++index, arg);
    } else if (arg === "--out") {
      parsed.out = requireValue(argv, ++index, arg);
    } else if (arg === "--fail-on-unknown") {
      parsed.failOnUnknown = true;
    } else if (arg === "--help" || arg === "-h") {
      printHelp();
      process.exit(0);
    } else {
      fail(`Unknown argument: ${arg}`);
    }
  }
  return parsed;
}

function requireValue(argv, index, flag) {
  const value = argv[index];
  if (!value || value.startsWith("--")) {
    fail(`${flag} requires a value`);
  }
  return value;
}

function printHelp() {
  console.log(`Usage: node scripts/generate-license-report.mjs [--project PATH] [--out PATH] [--fail-on-unknown]

Generates a Markdown dependency license report from go.mod and package-lock.json.
`);
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
