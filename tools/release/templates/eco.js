#!/usr/bin/env node
'use strict';

const { spawnSync } = require('node:child_process');

const SCOPE = '@{{.Scope}}';
const PLATFORM = process.platform;
const ARCH = process.arch;
const PACKAGE = `${SCOPE}/eco-${PLATFORM}-${ARCH}`;
const BINARY = PLATFORM === 'win32' ? 'bin/eco.exe' : 'bin/eco';

function fail(reason) {
  process.stderr.write(
    `eco: ${PACKAGE} is not installed on ${PLATFORM}/${ARCH}: ${reason}\n`
  );
  process.exit(2);
}

let target;
try {
  target = require.resolve(`${PACKAGE}/${BINARY}`);
} catch (error) {
  if (error && error.code === 'MODULE_NOT_FOUND') {
    fail('the platform package is missing; npm may have run with --no-optional, --omit=optional, an os/cpu mismatch, or an incomplete install');
  }
  fail(`${error && error.message}`);
}

const run = spawnSync(target, process.argv.slice(2), { stdio: 'inherit' });
if (run.error) {
  fail(`${run.error.message}`);
}
process.exit(run.status === null ? 1 : run.status);