import { createHash, randomUUID } from 'node:crypto';
import { promises as fs, constants } from 'node:fs';
import path from 'node:path';

export async function assertSafe(file) {
  let cursor = path.parse(file).root;
  for (const part of file.slice(cursor.length).split(path.sep).filter(Boolean)) {
    cursor = path.join(cursor, part);
    try { if ((await fs.lstat(cursor)).isSymbolicLink()) throw new Error(`Symlink paths are not supported: ${cursor}`); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
  }
}

export async function snapshot(file) {
  await assertSafe(file);
  let handle;
  try {
    const stat = await fs.lstat(file);
    if (!stat.isFile()) throw new Error(`Expected a regular file: ${file}`);
    if (stat.nlink > 1) throw new Error(`Hard-linked files are not supported: ${file}`);
    handle = await fs.open(file, constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0));
    const before = await handle.stat();
    if (!before.isFile() || before.nlink > 1) throw new Error('File identity changed while opening; retry');
    const data = await handle.readFile();
    const after = await handle.stat();
    if (before.mtimeMs !== after.mtimeMs || before.size !== after.size || before.mode !== after.mode) throw new Error('File changed while reading; retry');
    return { data: data.toString('base64'), mode: after.mode & 0o777 };
  } catch (error) { if (error.code === 'ENOENT') return null; throw error; }
  finally { await handle?.close(); }
}

export const fingerprint = value => value === null ? null : createHash('sha256').update(JSON.stringify([value.data, value.mode & 0o111])).digest('hex');

export async function writeSnapshot(file, value) {
  if (!value || typeof value.data !== 'string' || !Number.isInteger(value.mode) || value.mode < 0 || value.mode > 0o777) throw new Error('Invalid file snapshot');
  await assertSafe(file);
  await fs.mkdir(path.dirname(file), { recursive: true, mode: 0o700 });
  const temporary = `${file}.${randomUUID()}.tmp`;
  let handle;
  try {
    handle = await fs.open(temporary, 'wx', 0o600);
    await handle.writeFile(Buffer.from(value.data, 'base64'));
    await handle.chmod(value.mode);
    await handle.sync();
    await handle.close(); handle = undefined;
    await assertSafe(file);
    await fs.rename(temporary, file);
  } finally { await handle?.close(); await fs.rm(temporary, { force: true }); }
}

export async function walk(root) {
  await assertSafe(root);
  try { if (!(await fs.lstat(root)).isDirectory()) throw new Error(`Expected a skill directory: ${root}`); }
  catch (error) { if (error.code === 'ENOENT') return []; throw error; }
  const files = [];
  async function visit(directory) {
    for (const entry of await fs.readdir(directory, { withFileTypes: true })) {
      const file = path.join(directory, entry.name);
      if (entry.isSymbolicLink()) throw new Error(`Symlink paths are not supported: ${file}`);
      if (entry.isDirectory()) await visit(file);
      else if (entry.isFile()) files.push(path.relative(root, file));
      else throw new Error(`Unsupported special file: ${file}`);
    }
  }
  await visit(root);
  return files.sort();
}
