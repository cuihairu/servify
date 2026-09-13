import fs from "node:fs";
import path from "node:path";

const workspaceRoot = path.resolve(import.meta.dirname, "..");
const rootPkgPath = path.join(workspaceRoot, "package.json");
const publishablePackages = new Set([
  "@servify/core",
  "@servify/react",
  "@servify/vue",
  "@servify/vanilla",
  "@servify/api-client",
]);
const reservedPackages = new Set([
  "@servify/app-core",
]);
const checkOnly = process.argv.includes("--check");

function readJSON(filePath) {
  return JSON.parse(fs.readFileSync(filePath, "utf8"));
}

function writeJSON(filePath, data) {
  fs.writeFileSync(filePath, `${JSON.stringify(data, null, 2)}\n`);
}

function updateDependencyMap(deps, version) {
  if (!deps) return false;
  let changed = false;
  for (const depName of Object.keys(deps)) {
    if (publishablePackages.has(depName) && deps[depName] !== version) {
      deps[depName] = version;
      changed = true;
    }
  }
  return changed;
}

const rootPkg = readJSON(rootPkgPath);
const targetVersion = rootPkg.version;
const packagesDir = path.join(workspaceRoot, "packages");
const packageDirs = fs.readdirSync(packagesDir, { withFileTypes: true })
  .filter((entry) => entry.isDirectory())
  .map((entry) => path.join(packagesDir, entry.name));

let changedFiles = [];

for (const dir of packageDirs) {
  const pkgPath = path.join(dir, "package.json");
  const pkg = readJSON(pkgPath);
  let changed = false;

  if (publishablePackages.has(pkg.name) && pkg.version !== targetVersion) {
    pkg.version = targetVersion;
    changed = true;
  }

  if (reservedPackages.has(pkg.name) && pkg.version !== "0.0.0") {
    pkg.version = "0.0.0";
    changed = true;
  }

  if (updateDependencyMap(pkg.dependencies, targetVersion)) changed = true;
  if (updateDependencyMap(pkg.peerDependencies, targetVersion)) changed = true;
  if (updateDependencyMap(pkg.optionalDependencies, targetVersion)) changed = true;

  if (changed) {
    changedFiles.push(path.relative(workspaceRoot, pkgPath));
    if (!checkOnly) {
      writeJSON(pkgPath, pkg);
    }
  }
}

if (changedFiles.length > 0) {
  if (checkOnly) {
    console.error("SDK package versions are out of sync:");
    for (const file of changedFiles) {
      console.error(`- ${file}`);
    }
    process.exit(1);
  }
  console.log("Updated SDK package versions:");
  for (const file of changedFiles) {
    console.log(`- ${file}`);
  }
} else {
  console.log("SDK package versions are already in sync.");
}
