import { createHash, randomUUID } from 'node:crypto';
import { promises as fs } from 'node:fs';
import path from 'node:path';

const hash = bytes => bytes === null ? null : createHash('sha256').update(bytes).digest('hex');

async function read(file) {
  // Refuse symlinks in every existing path component, including parent directories.
  let cursor = path.parse(file).root;
  for (const part of file.slice(cursor.length).split(path.sep).filter(Boolean)) {
    cursor = path.join(cursor, part);
    try {
      if ((await fs.lstat(cursor)).isSymbolicLink()) throw new Error(`Symlink paths are not supported: ${cursor}`);
    } catch (error) { if (error.code !== 'ENOENT') throw error; }
  }
  try { return await fs.readFile(file); }
  catch (error) { if (error.code === 'ENOENT') return null; throw error; }
}

async function atomicWrite(file, bytes) {
  await fs.mkdir(path.dirname(file), { recursive: true, mode: 0o700 });
  const temporary = `${file}.${randomUUID()}.tmp`;
  try {
    await fs.writeFile(temporary, bytes, { flag: 'wx', mode: 0o600 });
    await fs.rename(temporary, file);
  } finally { await fs.rm(temporary, { force: true }); }
}

export async function loadConfig(filename) {
  const absolute = path.resolve(filename);
  const config = JSON.parse(await fs.readFile(absolute, 'utf8'));
  if (config.version !== 1 || !Array.isArray(config.resources) || !config.stateDir) {
    throw new Error('Expected version: 1, stateDir, and resources array');
  }
  const base = path.dirname(absolute);
  const stateDir = path.resolve(base, config.stateDir);
  const ids = new Set();
  const destinations = new Set();
  const resources = config.resources.map(resource => {
    if (!/^[a-zA-Z0-9_-]+$/.test(resource.id) || ids.has(resource.id)) throw new Error('Resource IDs must be unique and path-safe');
    ids.add(resource.id);
    if (resource.kind !== 'portable-file') throw new Error(`Unsupported adapter: ${resource.kind}`);
    if (!['global', 'project'].includes(resource.scope)) throw new Error('Each resource needs global or project scope');
    if (!resource.claude || !resource.codex) throw new Error('Each resource needs claude and codex paths');
    const paths = {
      shared: path.join(stateDir, 'shared', resource.id),
      claude: path.resolve(base, resource.claude),
      codex: path.resolve(base, resource.codex),
    };
    for (const side of ['claude', 'codex']) {
      const destination = paths[side];
      const relative = path.relative(stateDir, destination);
      if (!relative || (!relative.startsWith(`..${path.sep}`) && relative !== '..' && !path.isAbsolute(relative))) throw new Error('Native paths cannot be inside stateDir');
      if (destinations.has(destination)) throw new Error('Resources must not share destination paths');
      destinations.add(destination);
    }
    return { id: resource.id, scope: resource.scope, paths };
  });
  return { stateDir, resources };
}

export async function plan(config) {
  const manifestPath = path.join(config.stateDir, 'manifest.json');
  const manifestBytes = await read(manifestPath);
  const manifest = manifestBytes === null ? {} : JSON.parse(manifestBytes.toString());
  const items = [];
  for (const resource of config.resources) {
    const bytes = Object.fromEntries(await Promise.all(Object.entries(resource.paths).map(async ([side, file]) => [side, await read(file)])));
    const hashes = Object.fromEntries(Object.entries(bytes).map(([side, value]) => [side, hash(value)]));
    const baseline = manifest[resource.id];
    let reason;
    let selected;
    if (baseline === undefined) {
      const existing = Object.keys(bytes).filter(side => bytes[side] !== null);
      if (!existing.length) reason = 'No source exists';
      else if (new Set(existing.map(side => hashes[side])).size !== 1) reason = 'Initial contents differ; choose a shared version manually';
      else selected = existing[0];
    } else {
      const changed = Object.keys(hashes).filter(side => hashes[side] !== baseline);
      if (changed.some(side => hashes[side] === null)) reason = 'Deletion detected; automatic deletion is disabled';
      else if (new Set(changed.map(side => hashes[side])).size > 1) reason = 'Concurrent edits differ';
      else selected = changed[0] ?? 'shared';
    }
    const content = selected ? bytes[selected] : null;
    items.push({ ...resource, hashes, content, digest: hash(content), conflict: reason ?? null,
      writes: reason ? [] : Object.keys(hashes).filter(side => hashes[side] !== hash(content)),
    });
  }
  return { config, manifestBytes, manifest, items };
}

export function summarize(result) {
  return result.items.map(({ id, scope, conflict, writes }) => ({ id, scope, status: conflict ? 'conflict' : writes.length ? 'pending' : 'in-sync', writes, ...(conflict ? { reason: conflict } : {}) }));
}

export async function apply(config) {
  // Also validates existing state path components before creating the lock.
  await read(path.join(config.stateDir, 'manifest.json'));
  await fs.mkdir(config.stateDir, { recursive: true, mode: 0o700 });
  const lock = path.join(config.stateDir, 'sync.lock');
  let handle;
  try { handle = await fs.open(lock, 'wx', 0o600); }
  catch (error) { if (error.code === 'EEXIST') throw new Error('Another sync is active, or a stale sync.lock needs inspection'); throw error; }
  try {
    const result = await plan(config);
    if (result.items.some(item => item.conflict)) throw new Error(`Conflicts block all writes: ${result.items.filter(item => item.conflict).map(item => item.id).join(', ')}`);
    // Check all inputs before writing anything. External editors are not locked:
    // checks reduce races but are not a filesystem transaction.
    for (const item of result.items) {
      for (const [side, file] of Object.entries(item.paths)) {
        if (hash(await read(file)) !== item.hashes[side]) throw new Error(`Input changed during planning: ${item.id}`);
      }
    }
    const transaction = path.join(config.stateDir, 'backups', randomUUID());
    const journal = [];
    for (const item of result.items) {
      for (const side of item.writes) {
        const file = item.paths[side];
        const before = await read(file);
        if (hash(before) !== item.hashes[side]) throw new Error(`Input changed before write: ${item.id}`);
        const backup = path.join(transaction, `${item.id}-${side}`);
        if (before !== null) await atomicWrite(backup, before);
        journal.push({ file, backup: before === null ? null : backup });
        await atomicWrite(path.join(transaction, 'journal.json'), Buffer.from(JSON.stringify(journal, null, 2)));
        await atomicWrite(file, item.content);
      }
      result.manifest[item.id] = item.digest;
    }
    await atomicWrite(path.join(config.stateDir, 'manifest.json'), Buffer.from(JSON.stringify(result.manifest, null, 2)));
    return summarize(result);
  } finally {
    await handle.close();
    await fs.rm(lock, { force: true });
  }
}
