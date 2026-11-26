import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join } from "node:path";
import test, { type TestContext } from "node:test";

const installScript = "install_build_deps.sh";
const checker = "check_web_install_scripts.mjs";

function fixture(t: TestContext, allowScripts: object, packages: object) {
  const root = mkdtempSync(join(tmpdir(), "basket-install-policy-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));

  const scripts = join(root, ".github", "scripts");
  mkdirSync(scripts, { recursive: true });
  mkdirSync(join(root, "web"));
  for (const file of [installScript, checker]) {
    copyFileSync(new URL(`../../.github/scripts/${file}`, import.meta.url), join(scripts, file));
  }
  writeFileSync(join(root, "web", "package.json"), JSON.stringify({ allowScripts }));
  writeFileSync(join(root, "web", "package-lock.json"), JSON.stringify({ lockfileVersion: 3, packages }));
  return root;
}

test("accepts exact approvals and optional packages excluded by Linux OS rules", t => {
  const root = fixture(t, { "plain@1.0.0": true, "@scope/native@1.0.0": true }, {
    "node_modules/plain": { version: "1.0.0", hasInstallScript: true },
    "node_modules/parent/node_modules/@scope/native": { version: "1.0.0", hasInstallScript: true },
    "node_modules/alias": { name: "@scope/native", version: "1.0.0", hasInstallScript: true },
    "node_modules/darwin-only": { version: "1.0.0", hasInstallScript: true, optional: true, os: ["darwin"] },
    "node_modules/excludes-linux": { version: "1.0.0", hasInstallScript: true, optional: true, os: ["!linux"] },
  });
  const result = spawnSync(process.execPath, [`.github/scripts/${checker}`], { cwd: root, encoding: "utf8" });
  assert.equal(result.status, 0, result.stderr);
});

test("rejects unapproved install scripts even with a different version or an optional Linux package", t => {
  const root = fixture(t, { "plain@1.0.0": true, "denied@1.0.0": false }, {
    "node_modules/plain": { version: "2.0.0", hasInstallScript: true },
    "node_modules/denied": { version: "1.0.0", hasInstallScript: true },
    "node_modules/linux-only": { version: "1.0.0", hasInstallScript: true, optional: true, os: ["linux"] },
    "node_modules/required": { version: "1.0.0", hasInstallScript: true, os: ["darwin"] },
  });
  const result = spawnSync(process.execPath, [`.github/scripts/${checker}`], { cwd: root, encoding: "utf8" });
  assert.equal(result.status, 1, result.stderr);
  for (const identity of ["plain@2.0.0", "denied@1.0.0", "linux-only@1.0.0", "required@1.0.0"]) {
    assert.ok(result.stderr.includes(`Unapproved install script: ${identity} (`), result.stderr);
  }
});

test("the installer passes --ignore-scripts to npm ci", t => {
  const root = fixture(t, {}, {});
  const bin = join(root, "bin");
  mkdirSync(bin);
  writeFileSync(join(bin, "npm"), '#!/bin/sh\nprintf "%s\\n" "$@"\n', { mode: 0o755 });

  const result = spawnSync("sh", [`.github/scripts/${installScript}`], {
    cwd: root,
    encoding: "utf8",
    env: { ...process.env, PATH: `${bin}${delimiter}${process.env.PATH}` },
  });
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(result.stdout.trimEnd().split("\n"), ["ci", "--prefix", "web", "--ignore-scripts"]);
});
