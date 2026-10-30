// The action's main step: build the helper from the action's own source,
// then run its setup with every input passed through as a flag.
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { findGo, input, run } from './common.mjs';

const actionDir = dirname(dirname(fileURLToPath(import.meta.url)));

const runnerTemp = process.env.RUNNER_TEMP ?? '';
if (runnerTemp === '') {
  console.log('::error::rooket-action: RUNNER_TEMP is not set; the action runs only on a GitHub Actions runner');
  process.exit(1);
}
const binDir = join(runnerTemp, 'rooket-action', 'bin');
const helper = join(binDir, 'rooket-action');

const go = findGo();
if (go === '') {
  console.log('::error::rooket-action needs Go; add actions/setup-go before this step');
  process.exit(1);
}

// mustRun is run, with a command that did not run failing the step.
function mustRun(cmd, args, opts) {
  try {
    return run(cmd, args, opts);
  } catch (err) {
    console.log(`::error::rooket-action: ${err.message}`);
    return 1;
  }
}

mkdirSync(binDir, { recursive: true });
// The build must not see the consumer's Go environment: GOTOOLCHAIN=auto
// fetches the Go the helper's go.mod names, and a consumer's workspace,
// GOFLAGS (-mod=vendor, say) or cross-compilation target would break the
// build or produce a helper this runner cannot run. go reads an empty
// variable as unset and falls back to the go env -w file, so GOENV=off is
// what keeps the emptied ones empty.
const built = mustRun(go, ['build', '-o', helper, './cmd/rooket-action'], {
  cwd: actionDir,
  env: { ...process.env, GOTOOLCHAIN: 'auto', GOWORK: 'off', GOFLAGS: '', GOOS: '', GOARCH: '', GOENV: 'off' },
});
if (built !== 0) {
  process.exit(built);
}

// setup runs in the job's working directory, so that a relative rooket-path
// resolves against the workspace.
process.exit(
  mustRun(helper, [
    'setup',
    '--log-format=text',
    `--go=${go}`,
    `--bin-dir=${binDir}`,
    `--rooket-version=${input('rooket-version')}`,
    `--rooket-sha256=${input('rooket-sha256')}`,
    `--rooket-path=${input('rooket-path')}`,
    `--workers=${input('workers')}`,
    `--min-free-gb=${input('min-free-gb')}`,
    `--iscsi=${input('iscsi')}`,
    `--teardown=${input('teardown')}`,
  ]),
);
