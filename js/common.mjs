// Glue shared by the action's main and post steps. Every decision belongs to
// the Go helper; this only finds Go and passes the runner's values through.
import { spawnSync } from 'node:child_process';
import { accessSync, constants, statSync } from 'node:fs';
import { delimiter, resolve } from 'node:path';

// input returns the action input name as the runner passes it, or ''.
export function input(name) {
  return process.env[`INPUT_${name.toUpperCase()}`] ?? '';
}

// findGo returns the absolute path of the first executable go on PATH, or ''.
export function findGo() {
  for (const dir of (process.env.PATH ?? '').split(delimiter)) {
    if (dir === '') {
      continue;
    }
    const candidate = resolve(dir, 'go');
    try {
      if (statSync(candidate).isFile()) {
        accessSync(candidate, constants.X_OK);
        return candidate;
      }
    } catch {
      // Absent or not executable: keep looking.
    }
  }
  return '';
}

// run runs cmd with args, its output going to the step log, and returns its
// exit status. It throws when cmd does not run to an exit, because it cannot
// start or a signal kills it, so that each caller decides what that costs the
// job.
export function run(cmd, args, opts = {}) {
  const result = spawnSync(cmd, args, { stdio: 'inherit', ...opts });
  if (result.status === null) {
    const why = result.error ? result.error.message : `killed by ${result.signal}`;
    throw new Error(`running ${cmd}: ${why}`);
  }
  return result.status;
}
