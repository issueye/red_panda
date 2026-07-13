import { execFile, spawn } from 'node:child_process';
import { rm } from 'node:fs/promises';
import path from 'node:path';

const frontendRoot = process.cwd();
const repoRoot = path.resolve(frontendRoot, '..', '..', '..');
const gatewayExe = path.join(repoRoot, 'bin', process.platform === 'win32' ? 'red-panda-gateway.exe' : 'red-panda-gateway');
const agentExe = path.join(repoRoot, 'bin', process.platform === 'win32' ? 'red-panda-agent.exe' : 'red-panda-agent');
const databasePath = path.join(repoRoot, 'ui-e2e-red-panda.db');
const gatewayAddr = '127.0.0.1:17931';

/**
 * Env for Gateway-backed e2e (docs/36 C5).
 * Does NOT force RED_PANDA_SLASH_TOOLS — production default is off; tool paths
 * exercise the echo provider tool_call surface instead of Runtime Parse slash.
 *
 * @param {NodeJS.ProcessEnv} [baseEnv]
 * @param {Record<string, string>} [extraEnv]
 */
export function buildGatewayChildEnv(baseEnv = process.env, extraEnv = {}) {
  return {
    ...baseEnv,
    RED_PANDA_GATEWAY_ADDR: gatewayAddr,
    RED_PANDA_DATABASE: databasePath,
    RED_PANDA_AGENT_COMMAND: agentExe,
    ...extraEnv,
  };
}

export async function startGateway(options = {}) {
  const processBaseline = await captureManagedProcessBaseline();
  await removeDatabase();
  const extraEnv = options.env || options.extraEnv || {};
  const child = spawn(gatewayExe, {
    cwd: repoRoot,
    env: buildGatewayChildEnv(process.env, extraEnv),
    stdio: ['ignore', 'pipe', 'pipe'],
    detached: process.platform !== 'win32',
    windowsHide: true,
  });

  let stderr = '';
  child.stderr.on('data', (chunk) => {
    stderr += chunk.toString();
  });

  await waitForReady(child, stderr);
  return {
    baseURL: `http://${gatewayAddr}`,
    repoRoot,
    async stop() {
      await killProcessTree(child);
      await assertNoManagedProcessLeaks(processBaseline);
      await removeDatabase();
    },
  };
}

async function captureManagedProcessBaseline() {
  const processes = await listManagedProcesses();
  return new Set(processes.map((item) => item.pid));
}

async function assertNoManagedProcessLeaks(baseline) {
  const deadline = Date.now() + 3000;
  let leaked = [];
  while (Date.now() < deadline) {
    leaked = (await listManagedProcesses()).filter((item) => !baseline.has(item.pid));
    if (leaked.length === 0) {
      return;
    }
    await delay(150);
  }
  const summary = leaked
    .map((item) => `${item.pid}:${item.command || item.executable || 'red-panda process'}`)
    .join('; ');
  throw new Error(`red_panda process leak detected after Gateway-backed test: ${summary}`);
}

async function listManagedProcesses() {
  if (process.platform === 'win32') {
    return listManagedWindowsProcesses();
  }
  return listManagedPosixProcesses();
}

async function listManagedWindowsProcesses() {
  const script = `
$paths = @('${escapePowerShellString(gatewayExe)}','${escapePowerShellString(agentExe)}')
Get-CimInstance Win32_Process |
  Where-Object { $paths -contains $_.ExecutablePath } |
  Select-Object ProcessId,ParentProcessId,ExecutablePath,CommandLine |
  ConvertTo-Json -Compress
`;
  const output = await execFileText('powershell', ['-NoProfile', '-NonInteractive', '-Command', script]);
  return parseJsonProcessList(output).map((item) => ({
    pid: Number(item.ProcessId),
    parentPid: Number(item.ParentProcessId),
    executable: item.ExecutablePath || '',
    command: item.CommandLine || '',
  })).filter((item) => Number.isFinite(item.pid));
}

async function listManagedPosixProcesses() {
  const output = await execFileText('ps', ['-eo', 'pid=,ppid=,args=']);
  const managedNames = new Set([path.basename(gatewayExe), path.basename(agentExe)]);
  return output
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const match = line.match(/^(\d+)\s+(\d+)\s+(.+)$/);
      if (!match) {
        return null;
      }
      return {
        pid: Number(match[1]),
        parentPid: Number(match[2]),
        command: match[3],
      };
    })
    .filter((item) => item && Array.from(managedNames).some((name) => item.command.includes(name)));
}

function parseJsonProcessList(output) {
  const text = output.trim();
  if (!text) {
    return [];
  }
  const parsed = JSON.parse(text);
  return Array.isArray(parsed) ? parsed : [parsed];
}

function execFileText(command, args) {
  return new Promise((resolve, reject) => {
    execFile(command, args, { timeout: 7000, windowsHide: true }, (error, stdout, stderr) => {
      if (error) {
        reject(new Error(`${command} failed: ${stderr || error.message}`));
        return;
      }
      resolve(stdout.toString());
    });
  });
}

function escapePowerShellString(value) {
  return String(value).replace(/'/g, "''");
}

async function killProcessTree(child) {
  if (child.exitCode !== null || child.signalCode !== null || !child.pid) {
    return;
  }

  if (process.platform === 'win32') {
    await runTaskkill(child.pid);
  } else {
    try {
      process.kill(-child.pid, 'SIGTERM');
    } catch {
      killChild(child, 'SIGTERM');
    }
  }

  const exited = await waitForExit(child, 1500);
  if (exited) {
    return;
  }

  if (process.platform !== 'win32') {
    try {
      process.kill(-child.pid, 'SIGKILL');
    } catch {
      killChild(child, 'SIGKILL');
    }
  } else {
    killChild(child, 'SIGKILL');
  }

  await waitForExit(child, 1500);
}

function runTaskkill(pid) {
  return new Promise((resolve) => {
    const killer = spawn('taskkill', ['/PID', String(pid), '/T', '/F'], {
      stdio: 'ignore',
      windowsHide: true,
    });

    killer.once('error', resolve);
    killer.once('close', resolve);
  });
}

function killChild(child, signal) {
  try {
    child.kill(signal);
  } catch {
    // The process may already have exited between checks.
  }
}

function waitForExit(child, timeoutMs) {
  if (child.exitCode !== null || child.signalCode !== null) {
    return Promise.resolve(true);
  }

  return new Promise((resolve) => {
    const timeout = setTimeout(() => {
      cleanup();
      resolve(false);
    }, timeoutMs);
    const onExit = () => {
      cleanup();
      resolve(true);
    };
    const cleanup = () => {
      clearTimeout(timeout);
      child.off('exit', onExit);
    };

    child.once('exit', onExit);
  });
}

async function waitForReady(child, stderr) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      throw new Error(`gateway exited before ready: ${stderr}`);
    }
    try {
      const response = await fetch(`http://${gatewayAddr}/readyz`);
      if (response.ok) {
        return;
      }
    } catch {
      // Gateway is still starting.
    }
    await delay(150);
  }
  throw new Error(`gateway did not become ready: ${stderr}`);
}

async function removeDatabase() {
  await Promise.all([
    rm(databasePath, { force: true }),
    rm(`${databasePath}-shm`, { force: true }),
    rm(`${databasePath}-wal`, { force: true }),
  ]);
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
