#!/usr/bin/env node
const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

const binName = process.platform === 'win32' ? 'eco-win.cmd' : 'eco-win';

function findBin(startDir) {
  let dir = startDir;
  for (;;) {
    const candidate = path.join(dir, 'node_modules', '.bin', binName);
    if (fs.existsSync(candidate)) {
      return candidate;
    }
    const parent = path.dirname(dir);
    if (parent === dir) {
      return null;
    }
    dir = parent;
  }
}

const binPath = findBin(__dirname);
if (binPath === null) {
  process.stderr.write('eco: platform binary missing: ' + binName + '\n');
  process.exit(2);
}

let res;
if (process.platform === 'win32') {
  res = spawnSync(process.env.ComSpec || 'cmd.exe', ['/d', '/s', '/c', '""' + binPath + '""'], {
    stdio: 'inherit',
    windowsVerbatimArguments: true,
  });
} else {
  res = spawnSync(binPath, [], { stdio: 'inherit' });
}
process.exit(res.status === null ? 1 : res.status);