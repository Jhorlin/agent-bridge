#!/usr/bin/env node
import { setTimeout } from 'node:timers/promises';
import { loadConfig, plan, apply, summarize } from '../src/bridge.js';

const [command, filename, ...flags] = process.argv.slice(2);
if (!['plan', 'sync', 'watch'].includes(command) || !filename || flags.some(flag => flag !== '--apply') || (flags.length && command !== 'watch')) {
  console.error('Usage: agent-bridge <plan|sync|watch> <config.json> [--apply (watch only)]');
  process.exit(1);
}
let stopped = false;
process.on('SIGINT', () => { stopped = true; });
process.on('SIGTERM', () => { stopped = true; });
let last;
try {
  do {
    const config = await loadConfig(filename);
    const result = await plan(config);
    const summary = summarize(result);
    const conflict = summary.some(item => item.status === 'conflict');
    const output = JSON.stringify(summary, null, 2);
    if (output !== last) console.log(output);
    last = output;
    if (command === 'sync' || (command === 'watch' && flags.includes('--apply') && !conflict && summary.some(item => item.status === 'pending'))) await apply(config);
    if (command !== 'watch') { if (conflict) process.exitCode = 2; break; }
    await setTimeout(1000);
  } while (!stopped);
} catch (error) { console.error(error.message); process.exitCode = 1; }
