import { test } from 'node:test';
import assert from 'node:assert/strict';
import { promises as fs } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { setTimeout } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';
import { apply, loadConfig, plan, summarize } from '../src/bridge.js';

async function fixture(t) {
  const dir = await fs.mkdtemp(path.join(await fs.realpath(os.tmpdir()), 'agent-bridge-test-'));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  const filename = path.join(dir, 'config.json');
  await fs.writeFile(filename, JSON.stringify({ version: 1, stateDir: 'state', resources: [{ id: 'rules', kind: 'portable-file', scope: 'global', claude: 'CLAUDE.md', codex: 'AGENTS.md' }] }));
  await fs.writeFile(path.join(dir, 'CLAUDE.md'), 'one');
  return { dir, config: await loadConfig(filename), write: (file, text) => fs.writeFile(path.join(dir, file), text), read: file => fs.readFile(path.join(dir, file), 'utf8') };
}

test('plan is read-only', async t => {
  const f = await fixture(t);
  assert.equal(summarize(await plan(f.config))[0].status, 'pending');
  await assert.rejects(fs.stat(path.join(f.dir, 'state')), { code: 'ENOENT' });
});
test('bootstrap and repeated sync are idempotent', async t => {
  const f = await fixture(t); await apply(f.config);
  assert.equal(await f.read('AGENTS.md'), 'one');
  assert.equal(summarize(await plan(f.config))[0].status, 'in-sync');
});
test('edits flow from either native side or shared store', async t => {
  const f = await fixture(t); await apply(f.config);
  for (const file of ['AGENTS.md', 'CLAUDE.md', 'state/shared/rules']) {
    await f.write(file, file); await apply(f.config);
    assert.equal(await f.read('AGENTS.md'), file);
    assert.equal(await f.read('CLAUDE.md'), file);
  }
});
test('divergent initial files block writes', async t => {
  const f = await fixture(t); await f.write('AGENTS.md', 'different');
  await assert.rejects(apply(f.config), /Conflicts/);
  assert.equal(await f.read('CLAUDE.md'), 'one');
});
test('concurrent edits conflict and preserve both versions', async t => {
  const f = await fixture(t); await apply(f.config);
  await f.write('CLAUDE.md', 'left'); await f.write('AGENTS.md', 'right');
  await assert.rejects(apply(f.config), /Conflicts/);
  assert.equal(await f.read('CLAUDE.md'), 'left'); assert.equal(await f.read('AGENTS.md'), 'right');
});
test('identical concurrent edits converge', async t => {
  const f = await fixture(t); await apply(f.config);
  await f.write('CLAUDE.md', 'same'); await f.write('AGENTS.md', 'same');
  await apply(f.config); assert.equal(await f.read('state/shared/rules'), 'same');
});
test('deletion never propagates automatically', async t => {
  const f = await fixture(t); await apply(f.config);
  await fs.unlink(path.join(f.dir, 'CLAUDE.md'));
  await assert.rejects(apply(f.config), /Conflicts/);
  assert.equal(await f.read('AGENTS.md'), 'one');
});
test('symlink inputs are rejected', async t => {
  const f = await fixture(t);
  await fs.symlink(path.join(f.dir, 'CLAUDE.md'), path.join(f.dir, 'AGENTS.md'));
  await assert.rejects(plan(f.config), /Symlink/);
});
test('overwritten contents have a private backup journal', async t => {
  const f = await fixture(t); await apply(f.config);
  await f.write('CLAUDE.md', 'two'); await apply(f.config);
  const transactions = await fs.readdir(path.join(f.dir, 'state/backups'));
  const journals = await Promise.all(transactions.map(tx => f.read(`state/backups/${tx}/journal.json`)));
  assert.ok(journals.some(journal => journal.includes('rules-codex')));
});

test('an existing lock blocks a second writer', async t => {
  const f = await fixture(t); await apply(f.config);
  await f.write('state/sync.lock', '');
  await assert.rejects(apply(f.config), /Another sync/);
});

test('unsupported adapters fail explicitly', async t => {
  const f = await fixture(t);
  const config = JSON.parse(await f.read('config.json'));
  config.resources[0].kind = 'plugin';
  await f.write('config.json', JSON.stringify(config));
  await assert.rejects(loadConfig(path.join(f.dir, 'config.json')), /Unsupported adapter/);
});

test('watch --apply propagates edits and stops cleanly', { timeout: 10000 }, async t => {
  const f = await fixture(t);
  const child = spawn(process.execPath, [fileURLToPath(new URL('../bin/agent-bridge.js', import.meta.url)), 'watch', path.join(f.dir, 'config.json'), '--apply'], { stdio: 'ignore' });
  const exited = once(child, 'exit');
  t.after(() => { if (child.exitCode === null) child.kill('SIGKILL'); });
  async function waitFor(file, expected) {
    const deadline = Date.now() + 3500;
    while (Date.now() < deadline) {
      try { if (await f.read(file) === expected) return; } catch (error) { if (error.code !== 'ENOENT') throw error; }
      await setTimeout(30);
    }
    assert.fail(`Watch did not synchronize ${file}`);
  }
  await waitFor('AGENTS.md', 'one');
  await f.write('AGENTS.md', 'edited via codex');
  await waitFor('CLAUDE.md', 'edited via codex');
  child.kill('SIGTERM');
  const [code] = await exited;
  assert.equal(code, 0);
});
