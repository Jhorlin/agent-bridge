import { randomUUID } from 'node:crypto';
import { promises as fs } from 'node:fs';
import path from 'node:path';
import { snapshot, fingerprint, assertSafe, writeSnapshot, walk } from './filesystem.js';

const inside = (parent, child) => child === parent || child.startsWith(`${parent}${path.sep}`);
const manifestPath = config => path.join(config.stateDir, 'manifest.json');
const pendingPath = config => path.join(config.stateDir, 'pending.json');
const encoded = value => ({ data: Buffer.from(JSON.stringify(value, null, 2)).toString('base64'), mode: 0o600 });

export async function loadConfig(filename) {
  const absolute = path.resolve(filename);
  const raw = JSON.parse(await fs.readFile(absolute, 'utf8'));
  if (raw.version !== 1 || !Array.isArray(raw.resources) || typeof raw.stateDir !== 'string' || !raw.stateDir) throw new Error('Expected version: 1, stateDir, and resources array');
  const stateDir = path.resolve(path.dirname(absolute), raw.stateDir);
  const ids = new Set();
  const destinations = [stateDir, absolute];
  const resources = raw.resources.map(resource => {
    if (typeof resource.id !== 'string' || !/^[a-zA-Z0-9_-]+$/.test(resource.id) || Object.hasOwn(Object.prototype, resource.id) || resource.id === 'prototype' || ids.has(resource.id)) throw new Error('Resource IDs must be unique and path-safe');
    ids.add(resource.id);
    if (!['portable-file', 'skill-directory'].includes(resource.kind)) throw new Error(`Unsupported adapter: ${resource.kind}`);
    if (resource.kind === 'skill-directory' && resource.portable !== true) throw new Error('skill-directory requires portable: true after reviewing tool compatibility');
    if (!['global', 'project'].includes(resource.scope)) throw new Error('Each resource needs global or project scope');
    const paths = { shared: path.join(stateDir, 'shared', resource.id) };
    for (const side of ['claude', 'codex']) {
      if (typeof resource[side] !== 'string' || !resource[side] || resource[side].startsWith('~')) throw new Error('Use explicit absolute paths or config-relative paths, not tilde paths');
      const destination = path.resolve(path.dirname(absolute), resource[side]);
      if (destinations.some(other => inside(other.toLowerCase(), destination.toLowerCase()) || inside(destination.toLowerCase(), other.toLowerCase()))) throw new Error('Resource paths must not overlap each other, stateDir, or the config file (case-insensitive check)');
      destinations.push(destination);
      paths[side] = destination;
    }
    return { id: resource.id, kind: resource.kind, scope: resource.scope, paths };
  });
  return { stateDir, resources };
}

async function readManifest(config) {
  const before = await snapshot(manifestPath(config));
  if (!before) return { before, manifest: { version: 2, resources: {}, files: {} } };
  const manifest = JSON.parse(Buffer.from(before.data, 'base64').toString());
  if (manifest.version !== 2 || !manifest.resources || !manifest.files) throw new Error('Legacy or invalid manifest: preserve existing state and bootstrap into a fresh stateDir');
  for (const resource of config.resources) {
    const previous = manifest.resources[resource.id];
    if (previous && JSON.stringify(previous) !== JSON.stringify(resource)) throw new Error(`Managed resource changed identity: ${resource.id}; use a new ID and review adoption`);
  }
  return { before, manifest };
}

async function expand(resource, manifest) {
  if (resource.kind === 'portable-file') return [{ ...resource, key: resource.id }];
  const names = new Set();
  for (const root of Object.values(resource.paths)) {
    const files = await walk(root);
    if (files.length && !files.includes('SKILL.md')) throw new Error(`Skill directory must contain SKILL.md: ${resource.id}`);
    files.forEach(file => names.add(file));
  }
  const prefix = `${resource.id}/`;
  Object.keys(manifest.files).filter(key => key.startsWith(prefix)).forEach(key => names.add(key.slice(prefix.length)));
  if (!names.size) throw new Error(`No skill source exists: ${resource.id}`);
  return [...names].sort().map(name => {
    if (path.isAbsolute(name) || name.split(/[\\/]/).some(part => !part || part === '.' || part === '..')) throw new Error('Unsafe skill entry in manifest');
    return { ...resource, key: `${resource.id}/${name}`, relative: name, paths: Object.fromEntries(Object.entries(resource.paths).map(([side, root]) => [side, path.join(root, name)])) };
  });
}

export async function plan(config) {
  if (await snapshot(pendingPath(config))) throw new Error('An interrupted transaction requires recover before syncing');
  const { before, manifest } = await readManifest(config);
  const items = [];
  for (const resource of config.resources) {
    for (const entry of await expand(resource, manifest)) {
      const values = Object.fromEntries(await Promise.all(Object.entries(entry.paths).map(async ([side, file]) => [side, await snapshot(file)])));
      const hashes = Object.fromEntries(Object.entries(values).map(([side, value]) => [side, fingerprint(value)]));
      const baseline = manifest.files[entry.key];
      let conflict;
      let selected;
      if (baseline === undefined) {
        const existing = Object.keys(values).filter(side => values[side] !== null);
        if (!existing.length) conflict = 'No source exists';
        else if (new Set(existing.map(side => hashes[side])).size !== 1) conflict = 'Initial contents or executable bits differ';
        else selected = existing[0];
      } else {
        const changed = Object.keys(hashes).filter(side => hashes[side] !== baseline);
        if (changed.some(side => hashes[side] === null)) conflict = 'Deletion detected; automatic deletion is disabled';
        else if (new Set(changed.map(side => hashes[side])).size > 1) conflict = 'Concurrent edits differ';
        else selected = changed[0] ?? 'shared';
      }
      const content = selected ? values[selected] : null;
      const digest = fingerprint(content);
      items.push({ ...entry, values, hashes, content, digest, conflict: conflict ?? null,
        writes: conflict ? [] : Object.keys(hashes).filter(side => hashes[side] !== digest) });
    }
  }
  return { config, manifestBefore: before, manifest, items };
}

export function summarize(result) {
  return result.items.map(({ id, relative, scope, kind, conflict, writes }) => ({ id, ...(relative ? { file: relative } : {}), scope, kind,
    status: conflict ? 'conflict' : writes.length ? 'pending' : 'in-sync', writes, ...(conflict ? { reason: conflict } : {}) }));
}

async function locked(config, fn) {
  await assertSafe(config.stateDir);
  await fs.mkdir(config.stateDir, { recursive: true, mode: 0o700 });
  const lock = path.join(config.stateDir, 'sync.lock');
  let handle;
  try { handle = await fs.open(lock, 'wx', 0o600); }
  catch (error) { if (error.code === 'EEXIST') throw new Error('Another sync is active, or a stale sync.lock needs inspection'); throw error; }
  try {
    await handle.writeFile(JSON.stringify({ pid: process.pid, started: new Date().toISOString() }));
    return await fn();
  } finally { await handle.close(); await fs.unlink(lock); }
}

function allowedTarget(config, file) {
  if (file === manifestPath(config)) return true;
  return config.resources.some(resource => Object.values(resource.paths).some(root => resource.kind === 'portable-file' ? root === file : file !== root && inside(root, file)));
}

async function rollback(config, journal) {
  // Preflight every operation before restoring any file. Do not discard later edits.
  const seen = new Set();
  for (const op of journal.operations) {
    if (typeof op.file !== 'string' || path.resolve(op.file) !== op.file || !allowedTarget(config, op.file) || seen.has(op.file)) throw new Error('Recovery journal contains an invalid target');
    seen.add(op.file);
    const current = await snapshot(op.file);
    if (JSON.stringify(current) !== JSON.stringify(op.before) && JSON.stringify(current) !== JSON.stringify(op.after)) throw new Error(`Recovery blocked by a later edit: ${op.label}`);
  }
  for (const op of [...journal.operations].reverse()) {
    const current = await snapshot(op.file);
    if (JSON.stringify(current) === JSON.stringify(op.before)) continue;
    if (JSON.stringify(current) !== JSON.stringify(op.after)) throw new Error(`Recovery blocked by a later edit: ${op.label}`);
    if (op.before === null) await fs.unlink(op.file);
    else await writeSnapshot(op.file, op.before);
  }
  await fs.unlink(pendingPath(config));
}

export async function recover(config) {
  return locked(config, async () => {
    const pending = await snapshot(pendingPath(config));
    if (!pending) return { status: 'nothing-to-recover' };
    const { transaction } = JSON.parse(Buffer.from(pending.data, 'base64').toString());
    if (typeof transaction !== 'string' || !/^[a-f0-9-]{36}$/.test(transaction)) throw new Error('Invalid recovery transaction');
    const stored = await snapshot(path.join(config.stateDir, 'backups', transaction, 'journal.json'));
    if (!stored) throw new Error('Recovery journal is missing; manual inspection required');
    const journal = JSON.parse(Buffer.from(stored.data, 'base64').toString());
    if (journal.version !== 1 || !Array.isArray(journal.operations)) throw new Error('Invalid recovery journal');
    await rollback(config, journal);
    return { status: 'recovered', transaction };
  });
}

export async function apply(config, { beforeWrite } = {}) {
  return locked(config, async () => {
    const result = await plan(config);
    if (result.items.some(item => item.conflict)) throw new Error(`Conflicts block all writes: ${result.items.filter(item => item.conflict).map(item => item.key).join(', ')}`);
    const operations = [];
    for (const item of result.items) {
      for (const [side, file] of Object.entries(item.paths)) {
        if (JSON.stringify(await snapshot(file)) !== JSON.stringify(item.values[side])) throw new Error(`Input changed during planning: ${item.key}`);
      }
      for (const side of item.writes) {
        const before = item.values[side];
        const mode = ((before?.mode ?? 0o600) & 0o666) | (item.content.mode & 0o111);
        operations.push({ label: `${item.key}-${side}`, file: item.paths[side], before, after: { data: item.content.data, mode } });
      }
      result.manifest.files[item.key] = item.digest;
    }
    config.resources.forEach(resource => { result.manifest.resources[resource.id] = resource; });
    const afterManifest = encoded(result.manifest);
    if (JSON.stringify(result.manifestBefore) !== JSON.stringify(afterManifest)) operations.push({ label: 'manifest', file: manifestPath(config), before: result.manifestBefore, after: afterManifest });
    if (!operations.length) return summarize(result);
    const transaction = randomUUID();
    const journal = { version: 1, operations };
    await writeSnapshot(path.join(config.stateDir, 'backups', transaction, 'journal.json'), encoded(journal));
    await writeSnapshot(pendingPath(config), encoded({ transaction }));
    try {
      for (const [index, op] of operations.entries()) {
        await beforeWrite?.(index, op); // Fault injection for isolated tests; not a CLI option.
        if (JSON.stringify(await snapshot(op.file)) !== JSON.stringify(op.before)) throw new Error(`Input changed before write: ${op.label}`);
        await writeSnapshot(op.file, op.after);
      }
      await fs.unlink(pendingPath(config));
    } catch (error) {
      try { await rollback(config, journal); }
      catch (recoveryError) { throw new Error(`${error.message}; ${recoveryError.message}. Pending transaction retained; run recover after inspection.`, { cause: error }); }
      throw new Error(`${error.message}; transaction rolled back`, { cause: error });
    }
    return summarize(result);
  });
}
