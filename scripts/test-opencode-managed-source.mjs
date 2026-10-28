// Executed with the reviewed Bun after frozen resolution; sentry requests are counted.
// This source probe is not network-isolated; the binary gate provides isolation.
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { createServer } from "node:http";
import { createRequire } from "node:module";

const source = process.argv[2];
const root = await fs.mkdtemp(path.join(os.tmpdir(), "ivoai-managed-source-"));
await fs.chmod(root, 0o700);
const assets = path.join(root, "assets");
await fs.mkdir(assets, { mode: 0o700 });
process.env.IVOAI_MANAGED_ASSETS_DIR = assets;
for (const key of ["HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"]) {
  process.env[key] = path.join(root, key.toLowerCase());
  await fs.mkdir(process.env[key], { mode: 0o700 });
}
const helper = await import(pathToFileURL(path.join(source, "packages/opencode/src/plugin/ivoai-managed.ts")).href);
const plugin = path.join(assets, "server-plugin.mjs");
const tui = path.join(assets, "tui-plugin.tsx");
await fs.writeFile(plugin, "export default async () => ({})\n", { mode: 0o600 });
await fs.writeFile(tui, "export default {}\n", { mode: 0o600 });
try {
  if (process.env.IVOAI_MANAGED_LOCAL_PLUGINS !== "1") {
    await helper.assertIvoaiManagedPlugin("@fixture/foreign", "server");
    await helper.assertIvoaiManagedPlugin("https://fixture.invalid/plugin", "tui");
    console.log("IVOAI_UNFLAGGED_PLUGIN_BEHAVIOR=PASS");
  } else {
    await helper.assertIvoaiManagedPlugin(pathToFileURL(plugin).href, "server");
    await helper.assertIvoaiManagedPlugin(pathToFileURL(tui).href, "tui");
    for (const spec of ["@fixture/foreign", "https://fixture.invalid/plugin", pathToFileURL(tui).href, pathToFileURL(plugin).href + "?foreign=1"]) {
      await assert.rejects(helper.assertIvoaiManagedPlugin(spec, "server"), /IVOAI_MANAGED_PLUGIN_DENIED/);
    }
    await fs.chmod(plugin, 0o644);
    await assert.rejects(helper.assertIvoaiManagedPlugin(pathToFileURL(plugin).href, "server"), /IVOAI_MANAGED_PLUGIN_DENIED/);
    await fs.chmod(plugin, 0o600);
    await fs.rename(plugin, path.join(root, "foreign.mjs"));
    await fs.symlink(path.join(root, "foreign.mjs"), plugin);
    await assert.rejects(helper.assertIvoaiManagedPlugin(pathToFileURL(plugin).href, "server"), /IVOAI_MANAGED_PLUGIN_DENIED/);
    const alias = path.join(root, "alias");
    await fs.symlink(assets, alias);
    process.env.IVOAI_MANAGED_ASSETS_DIR = alias;
    await assert.rejects(helper.assertIvoaiManagedPlugin(pathToFileURL(path.join(alias, "tui-plugin.tsx")).href, "tui"), /IVOAI_MANAGED_PLUGIN_DENIED/);
    process.env.IVOAI_MANAGED_ASSETS_DIR = assets;
    let requests = 0;
    const registry = createServer((_req, res) => { requests++; res.writeHead(500); res.end(); });
    await new Promise(resolve => registry.listen(0, "127.0.0.1", resolve));
    const npmRoot = path.join(root, "npm");
    await fs.mkdir(npmRoot, { mode: 0o700 });
    process.env.npm_config_registry = `http://127.0.0.1:${registry.address().port}`;
    const deniedNpm = error => {
      assert.equal(error._tag, "NpmInstallFailedError");
      assert.equal(error.cause, "IVOAI_MANAGED_NPM_INSTALL_DENIED");
      return true;
    };
    try {
      const npm = await import(pathToFileURL(path.join(source, "packages/core/src/npm.ts")).href);
      const require = createRequire(path.join(source, "packages/core/package.json"));
      const { ManagedRuntime } = await import(pathToFileURL(require.resolve("effect")).href);
      const { LayerNode } = await import(pathToFileURL(path.join(source, "packages/core/src/effect/layer-node.ts")).href);
      const runtime = ManagedRuntime.make(LayerNode.compile(npm.node));
      try {
        await assert.rejects(runtime.runPromise(npm.Service.use(service => service.install(npmRoot, { add: [{ name: "@ivoai-fixture/never-install", version: "1.0.0" }] }))), deniedNpm);
        await assert.rejects(runtime.runPromise(npm.Service.use(service => service.add("@ivoai-fixture/never-install@1.0.0"))), deniedNpm);
      } finally {
        await runtime.dispose();
      }
      assert.equal(requests, 0, "managed SDK/package paths contacted the registry before denial");
      assert.equal(await fs.stat(path.join(npmRoot, "node_modules")).then(() => true, () => false), false);
    } finally {
      await new Promise(resolve => registry.close(resolve));
    }
    console.log("IVOAI_MANAGED_LOCAL_PLUGIN_AND_NPM_GATES=PASS registry_requests=0");
  }
} finally {
  await fs.rm(root, { recursive: true, force: true });
}

// The test owns and disposes its Effect runtime; no forced successful exit.
