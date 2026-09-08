import { test } from 'node:test';
import assert from 'node:assert/strict';
import { promises as fs } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { setTimeout } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';
import { apply, loadConfig, plan, recover, summarize } from '../src/bridge.js';

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

async function skillFixture(t) {
  const f = await fixture(t);
  await fs.mkdir(path.join(f.dir, 'claude-skill/scripts'), { recursive: true });
  await f.write('claude-skill/SKILL.md', '---\nname: demo\ndescription: A portable demo.\n---\nUse scripts/run.sh.\n');
  await f.write('claude-skill/scripts/run.sh', '#!/bin/sh\nprintf demo\n');
  await fs.chmod(path.join(f.dir, 'claude-skill/scripts/run.sh'), 0o755);
  await f.write('config.json', JSON.stringify({ version: 1, stateDir: 'state', resources: [{ id: 'demo', kind: 'skill-directory', portable: true, scope: 'global', claude: 'claude-skill', codex: 'codex-skill' }] }));
  f.config = await loadConfig(path.join(f.dir, 'config.json'));
  return f;
}

test('skill directories preserve nested bytes and executable bits', async t => {
  const f = await skillFixture(t);
  await fs.writeFile(path.join(f.dir, 'claude-skill/binary'), Buffer.from([0, 255, 3]));
  await apply(f.config);
  assert.equal(await f.read('codex-skill/SKILL.md'), await f.read('claude-skill/SKILL.md'));
  assert.deepEqual(await fs.readFile(path.join(f.dir, 'codex-skill/binary')), Buffer.from([0, 255, 3]));
  assert.equal((await fs.stat(path.join(f.dir, 'codex-skill/scripts/run.sh'))).mode & 0o111, 0o111);
  assert.ok(summarize(await plan(f.config)).every(item => item.status === 'in-sync'));
});

test('independent edits and new skill files converge from both sides', async t => {
  const f = await skillFixture(t); await apply(f.config);
  await f.write('claude-skill/SKILL.md', 'updated portable instructions');
  await f.write('codex-skill/scripts/run.sh', '#!/bin/sh\nprintf updated\n');
  await f.write('codex-skill/new.txt', 'new support file');
  await apply(f.config);
  assert.equal(await f.read('codex-skill/SKILL.md'), 'updated portable instructions');
  assert.equal(await f.read('claude-skill/new.txt'), 'new support file');
  assert.equal(await f.read('claude-skill/scripts/run.sh'), '#!/bin/sh\nprintf updated\n');
});

test('conflict in one skill file blocks all other writes', async t => {
  const f = await skillFixture(t); await apply(f.config);
  await f.write('claude-skill/SKILL.md', 'left'); await f.write('codex-skill/SKILL.md', 'right');
  await f.write('codex-skill/new.txt', 'unrelated');
  await assert.rejects(apply(f.config), /Conflicts/);
  await assert.rejects(f.read('claude-skill/new.txt'), { code: 'ENOENT' });
});

test('deleting a skill support file is a conflict, not a prune', async t => {
  const f = await skillFixture(t); await apply(f.config);
  await fs.unlink(path.join(f.dir, 'codex-skill/scripts/run.sh'));
  await assert.rejects(apply(f.config), /Conflicts/);
  assert.match(await f.read('claude-skill/scripts/run.sh'), /demo/);
});

test('skill symlinks and missing SKILL.md fail explicitly', async t => {
  const f = await skillFixture(t);
  await fs.symlink(path.join(f.dir, 'CLAUDE.md'), path.join(f.dir, 'claude-skill/link'));
  await assert.rejects(plan(f.config), /Symlink/);
  await fs.unlink(path.join(f.dir, 'claude-skill/link'));
  await fs.unlink(path.join(f.dir, 'claude-skill/SKILL.md'));
  await assert.rejects(plan(f.config), /SKILL.md/);
});

test('skill adapter requires explicit portability acknowledgement', async t => {
  const f = await skillFixture(t);
  const raw = JSON.parse(await f.read('config.json')); delete raw.resources[0].portable;
  await f.write('config.json', JSON.stringify(raw));
  await assert.rejects(loadConfig(path.join(f.dir, 'config.json')), /portable: true/);
});

test('mode-only changes propagate executable bits without widening rw access', async t => {
  const f = await fixture(t); await apply(f.config);
  await fs.chmod(path.join(f.dir, 'CLAUDE.md'), 0o744);
  await apply(f.config);
  assert.equal((await fs.stat(path.join(f.dir, 'AGENTS.md'))).mode & 0o777, 0o700);
});

test('injected partial-write failure rolls back files and baseline', async t => {
  const f = await fixture(t); await apply(f.config);
  const manifest = await f.read('state/manifest.json');
  await f.write('CLAUDE.md', 'new edit');
  await assert.rejects(apply(f.config, { beforeWrite: index => { if (index === 1) throw new Error('Injected failure'); } }), /rolled back/);
  assert.equal(await f.read('AGENTS.md'), 'one');
  assert.equal(await f.read('state/shared/rules'), 'one');
  assert.equal(await f.read('state/manifest.json'), manifest);
  assert.equal(await f.read('CLAUDE.md'), 'new edit');
  assert.deepEqual(await recover(f.config), { status: 'nothing-to-recover' });
  await apply(f.config); assert.equal(await f.read('AGENTS.md'), 'new edit');
});

test('failed bootstrap removes only files created by that transaction', async t => {
  const f = await fixture(t);
  await assert.rejects(apply(f.config, { beforeWrite: index => { if (index === 1) throw new Error('fail'); } }), /rolled back/);
  assert.equal(await f.read('CLAUDE.md'), 'one');
  await assert.rejects(f.read('state/shared/rules'), { code: 'ENOENT' });
  await assert.rejects(f.read('AGENTS.md'), { code: 'ENOENT' });
});

test('later edits block rollback and explicit recovery until resolved', async t => {
  const f = await fixture(t); await apply(f.config); await f.write('CLAUDE.md', 'new');
  await assert.rejects(apply(f.config, { beforeWrite: async index => {
    if (index === 1) { await f.write('state/shared/rules', 'later edit'); throw new Error('fail'); }
  } }), /Pending transaction retained/);
  await assert.rejects(recover(f.config), /later edit/);
  await assert.rejects(plan(f.config), /requires recover/);
  assert.equal(await f.read('state/shared/rules'), 'later edit');
  await f.write('state/shared/rules', 'new');
  assert.equal((await recover(f.config)).status, 'recovered');
  assert.equal(await f.read('state/shared/rules'), 'one');
});

test('process interruption is recoverable after inspecting the stale lock', async t => {
  const f = await fixture(t);
  const moduleUrl = new URL('../src/bridge.js', import.meta.url).href;
  const child = spawn(process.execPath, ['--input-type=module', '-e', `import {loadConfig,apply} from ${JSON.stringify(moduleUrl)}; await apply(await loadConfig(process.argv[1]), {beforeWrite(index) {if(index===1) process.exit(91)}});`, path.join(f.dir, 'config.json')], { stdio: 'ignore' });
  const [code] = await once(child, 'exit'); assert.equal(code, 91);
  await assert.rejects(recover(f.config), /stale sync.lock/);
  // Child has exited; remove only this fixture's known stale lock.
  await fs.unlink(path.join(f.dir, 'state/sync.lock'));
  assert.equal((await recover(f.config)).status, 'recovered');
  await assert.rejects(f.read('state/shared/rules'), { code: 'ENOENT' });
  await apply(f.config); assert.equal(await f.read('AGENTS.md'), 'one');
});

test('nested resource paths are rejected', async t => {
  const f = await skillFixture(t);
  const raw = JSON.parse(await f.read('config.json'));
  raw.resources.push({ id: 'nested', kind: 'portable-file', scope: 'project', claude: 'claude-skill/SKILL.md', codex: 'elsewhere' });
  await f.write('config.json', JSON.stringify(raw));
  await assert.rejects(loadConfig(path.join(f.dir, 'config.json')), /overlap/);
});

test('changing a managed resource path requires new adoption', async t => {
  const f = await fixture(t); await apply(f.config);
  const raw = JSON.parse(await f.read('config.json')); raw.resources[0].codex = 'other.md';
  await f.write('config.json', JSON.stringify(raw));
  await assert.rejects(plan(await loadConfig(path.join(f.dir, 'config.json'))), /changed identity/);
});

test('in-sync apply does not create extra backup transactions', async t => {
  const f = await fixture(t); await apply(f.config);
  const before = await fs.readdir(path.join(f.dir, 'state/backups'));
  await apply(f.config);
  assert.deepEqual(await fs.readdir(path.join(f.dir, 'state/backups')), before);
});

test('hard-linked peer files are rejected', async t => {
  const f = await fixture(t);
  await fs.link(path.join(f.dir, 'CLAUDE.md'), path.join(f.dir, 'AGENTS.md'));
  await assert.rejects(plan(f.config), /Hard-linked/);
});

test('reserved resource IDs cannot collide with object prototypes', async t => {
  const f = await fixture(t);
  const raw = JSON.parse(await f.read('config.json')); raw.resources[0].id = 'toString';
  await f.write('config.json', JSON.stringify(raw));
  await assert.rejects(loadConfig(path.join(f.dir, 'config.json')), /path-safe/);
});

test('recovery refuses journal targets outside the configured resources', async t => {
  const f = await fixture(t);
  await f.write('unmanaged.txt', 'preserve me');
  const transaction = '11111111-1111-1111-1111-111111111111';
  await fs.mkdir(path.join(f.dir, 'state/backups', transaction), { recursive: true });
  await f.write('state/pending.json', JSON.stringify({ transaction }));
  await f.write(`state/backups/${transaction}/journal.json`, JSON.stringify({ version: 1, operations: [{ file: path.join(f.dir, 'unmanaged.txt'), before: null, after: { data: Buffer.from('preserve me').toString('base64'), mode: 0o644 } }] }));
  await assert.rejects(recover(f.config), /invalid target/);
  assert.equal(await f.read('unmanaged.txt'), 'preserve me');
});

test('CLI sync reports conflicts with exit code 2 and no writes', async t => {
  const f = await fixture(t); await f.write('AGENTS.md', 'different');
  const child = spawn(process.execPath, [fileURLToPath(new URL('../bin/agent-bridge.js', import.meta.url)), 'sync', path.join(f.dir, 'config.json')], { stdio: 'ignore' });
  const [code] = await once(child, 'exit'); assert.equal(code, 2);
  await assert.rejects(fs.stat(path.join(f.dir, 'state')), { code: 'ENOENT' });
});
