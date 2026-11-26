import { readFileSync } from "node:fs";

const { allowScripts = {} } = JSON.parse(readFileSync(new URL("../../web/package.json", import.meta.url), "utf8"));
const { packages } = JSON.parse(readFileSync(new URL("../../web/package-lock.json", import.meta.url), "utf8"));

// Match npm's OS list against the Linux CI target on every host.
function supportsLinux(os = []) {
  const platforms = typeof os === "string" ? [os] : os;
  if (platforms.length === 1 && platforms[0] === "any") return true;
  if (platforms.includes("!linux")) return false;
  return platforms.includes("linux") || platforms.every(platform => platform.startsWith("!"));
}

for (const [location, entry] of Object.entries(packages)) {
  if (!location || entry.hasInstallScript !== true) continue;
  if (entry.optional === true && !supportsLinux(entry.os)) continue;

  // Aliases record the real package name. Otherwise the final node_modules
  // segment includes the full name, even for scoped and nested packages.
  const name = entry.name ?? location.split(/(?:^|\/)node_modules\//).at(-1);
  const identity = `${name}@${entry.version}`;
  if (allowScripts[identity] === true) continue;

  console.error(`Unapproved install script: ${identity} (${location}). Review web/package.json's allowScripts.`);
  process.exitCode = 1;
}
