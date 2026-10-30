// The action's post step: tear down what rooket left behind, through the
// helper that main's setup recorded.
import { input, run } from './common.mjs';

// setup records helper last, so without it setup did not finish, and there
// is nothing of the action's to tear down.
const helper = process.env.STATE_helper ?? '';
if (helper === '') {
  console.log('rooket-action: setup did not finish, so there is nothing to tear down');
  process.exit(0);
}

// The helper reports a failed teardown as a warning and exits non-zero only
// for an invalid teardown input, so its status passes through. A helper that
// did not run is a failed teardown too: by now the job's result is decided,
// and the runner is discarded after it.
let status;
try {
  status = run(helper, [
    'teardown',
    '--log-format=text',
    `--rooket=${process.env.STATE_rooket ?? ''}`,
    `--teardown=${input('teardown')}`,
  ]);
} catch (err) {
  console.log(`::warning::rooket-action: not tearing down rooket clusters: ${err.message}`);
  status = 0;
}
process.exit(status);
