#!/usr/bin/env node

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const schemaVersion = "poolnext.baseline.metric/v1";
const publicPaths = new Set(["/about", "/login", "/register", "/reset"]);

function parseArgs(argv) {
  const result = {
    output: "-",
    viewport: "1440x900",
    row_selector: '[role="row"],tr,[data-virtual-row]',
    storage_state: "",
    git_commit: "",
    expected_url: "",
    ready_selector: "main",
    authenticated_selector: "",
    timeout_ms: "30000",
    lock_timeout_ms: "10000",
  };
  for (let index = 0; index < argv.length; index += 1) {
    const key = argv[index];
    if (!key.startsWith("--")) throw new Error(`unexpected argument: ${key}`);
    const value = argv[index + 1];
    if (value === undefined || value.startsWith("--"))
      throw new Error(`missing value for ${key}`);
    result[key.slice(2).replaceAll("-", "_")] = value;
    index += 1;
  }
  for (const required of ["url", "run_id", "scenario"]) {
    if (!result[required])
      throw new Error(`--${required.replaceAll("_", "-")} is required`);
  }
  if (result.git_commit && !/^[0-9a-f]{7,64}$/.test(result.git_commit)) {
    throw new Error(
      "--git-commit must contain 7-64 lowercase hexadecimal characters",
    );
  }
  result.timeout_ms = positiveInteger(result.timeout_ms, "--timeout-ms");
  result.lock_timeout_ms = positiveInteger(
    result.lock_timeout_ms,
    "--lock-timeout-ms",
  );
  return result;
}

function resolvePlaywright() {
  const scriptDir = path.dirname(fileURLToPath(import.meta.url));
  const repoRoot = path.resolve(scriptDir, "..", "..");
  const requireFromWeb = createRequire(
    path.join(repoRoot, "server", "web", "package.json"),
  );
  try {
    return requireFromWeb("@playwright/test");
  } catch (error) {
    throw new Error(
      `Playwright is not installed under server/web; install the locked web dependencies first (${error.message})`,
    );
  }
}

function positiveInteger(raw, label) {
  if (!/^\d+$/.test(String(raw)))
    throw new Error(`${label} must be a positive integer`);
  const value = Number(raw);
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new Error(`${label} must be a positive integer`);
  }
  return value;
}

function parseViewport(raw) {
  const match = /^(\d+)x(\d+)$/.exec(raw);
  if (!match) throw new Error(`invalid viewport ${raw}; expected WIDTHxHEIGHT`);
  return {
    width: positiveInteger(match[1], "viewport width"),
    height: positiveInteger(match[2], "viewport height"),
  };
}

function utcNow() {
  return new Date().toISOString();
}

function normalizedPathname(value) {
  if (value === "/") return value;
  return value.replace(/\/+$/, "") || "/";
}

export function sanitizedPageUrl(raw) {
  const parsed = new URL(raw);
  return `${parsed.origin}${normalizedPathname(parsed.pathname)}`;
}

export function assertExpectedFinalUrl(actualRaw, expectedRaw) {
  const actual = new URL(actualRaw);
  const expected = new URL(expectedRaw);
  if (
    actual.origin !== expected.origin ||
    normalizedPathname(actual.pathname) !==
      normalizedPathname(expected.pathname)
  ) {
    throw new Error(
      `unexpected final URL: expected ${sanitizedPageUrl(expectedRaw)}, got ${sanitizedPageUrl(actualRaw)}`,
    );
  }
}

function requiresAuthentication(raw) {
  const pathname = normalizedPathname(new URL(raw).pathname);
  if (publicPaths.has(pathname)) return false;
  return !pathname.startsWith("/oauth/") && !pathname.startsWith("/user/reset");
}

function metric(args, name, value, unit, dimensions) {
  if (!Number.isFinite(value) || value < 0) {
    throw new Error(`metric ${name} must be a finite non-negative number`);
  }
  const record = {
    schema_version: schemaVersion,
    run_id: args.run_id,
    scenario: args.scenario,
    captured_at: utcNow(),
    metric: name,
    value,
    unit,
    sample_count: 1,
    dimensions,
  };
  if (args.git_commit) record.git_commit = args.git_commit;
  return record;
}

function rejectSymlinkComponents(rawPath) {
  const absolute = path.resolve(rawPath);
  const parsed = path.parse(absolute);
  let current = parsed.root;
  for (const component of absolute
    .slice(parsed.root.length)
    .split(path.sep)
    .filter(Boolean)) {
    current = path.join(current, component);
    try {
      if (fs.lstatSync(current).isSymbolicLink()) {
        throw new Error(`output path must not contain symlinks: ${current}`);
      }
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
  }
  return absolute;
}

function sleepSync(milliseconds) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, milliseconds);
}

function acquireOutputLock(output, timeoutMs) {
  const lockPath = `${output}.lock`;
  const flags =
    fs.constants.O_WRONLY |
    fs.constants.O_CREAT |
    fs.constants.O_EXCL |
    (fs.constants.O_NOFOLLOW ?? 0);
  const deadline = Date.now() + timeoutMs;
  while (true) {
    try {
      const descriptor = fs.openSync(lockPath, flags, 0o600);
      const info = fs.fstatSync(descriptor);
      if (!info.isFile()) {
        fs.closeSync(descriptor);
        throw new Error(`JSONL output lock is not a regular file: ${lockPath}`);
      }
      fs.writeSync(descriptor, `pid=${process.pid}\n`, null, "utf8");
      fs.fsyncSync(descriptor);
      return { descriptor, lockPath, device: info.dev, inode: info.ino };
    } catch (error) {
      if (error.code !== "EEXIST") throw error;
      if (Date.now() >= deadline) {
        throw new Error(`timed out waiting for JSONL output lock: ${lockPath}`);
      }
      sleepSync(10);
    }
  }
}

function releaseOutputLock(lock) {
  fs.closeSync(lock.descriptor);
  try {
    const current = fs.lstatSync(lock.lockPath);
    if (current.dev === lock.device && current.ino === lock.inode)
      fs.unlinkSync(lock.lockPath);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
}

function writeAll(descriptor, payload) {
  let offset = 0;
  while (offset < payload.length) {
    const written = fs.writeSync(
      descriptor,
      payload,
      offset,
      payload.length - offset,
    );
    if (written <= 0)
      throw new Error("short write while appending baseline JSONL");
    offset += written;
  }
}

function assertFiniteJson(value, location = "record") {
  if (typeof value === "number" && !Number.isFinite(value)) {
    throw new Error(`${location} contains a non-finite number`);
  }
  if (Array.isArray(value)) {
    value.forEach((item, index) =>
      assertFiniteJson(item, `${location}[${index}]`),
    );
    return;
  }
  if (value && typeof value === "object") {
    for (const [key, item] of Object.entries(value)) {
      assertFiniteJson(item, `${location}.${key}`);
    }
  }
}

export function writeJsonl(output, records, lockTimeoutMs = 10000) {
  records.forEach((record, index) =>
    assertFiniteJson(record, `record[${index}]`),
  );
  const text = `${records.map((record) => JSON.stringify(record)).join("\n")}\n`;
  if (output === "-") {
    process.stdout.write(text);
    return;
  }
  let destination = rejectSymlinkComponents(output);
  fs.mkdirSync(path.dirname(destination), { recursive: true });
  destination = rejectSymlinkComponents(destination);
  const lock = acquireOutputLock(destination, lockTimeoutMs);
  try {
    const flags =
      fs.constants.O_WRONLY |
      fs.constants.O_CREAT |
      fs.constants.O_APPEND |
      (fs.constants.O_NOFOLLOW ?? 0) |
      (fs.constants.O_NONBLOCK ?? 0);
    const descriptor = fs.openSync(destination, flags, 0o600);
    try {
      if (!fs.fstatSync(descriptor).isFile()) {
        throw new Error(`JSONL output is not a regular file: ${destination}`);
      }
      writeAll(descriptor, Buffer.from(text, "utf8"));
      fs.fsyncSync(descriptor);
    } finally {
      fs.closeSync(descriptor);
    }
  } finally {
    releaseOutputLock(lock);
  }
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const { chromium } = resolvePlaywright();
  const viewport = parseViewport(args.viewport);
  const expectedUrl = args.expected_url || args.url;
  const authenticatedSelector =
    args.authenticated_selector ||
    (requiresAuthentication(expectedUrl) ? "aside" : "");
  const browser = await chromium.launch({ headless: true });
  let context;
  try {
    const contextOptions = { viewport };
    if (args.storage_state) contextOptions.storageState = args.storage_state;
    context = await browser.newContext(contextOptions);
    const page = await context.newPage();
    const response = await page.goto(args.url, {
      waitUntil: "domcontentloaded",
      timeout: args.timeout_ms,
    });
    if (!response)
      throw new Error(`navigation produced no HTTP response: ${args.url}`);
    if (response.status() >= 400) {
      throw new Error(
        `navigation failed with HTTP ${response.status()}: ${args.url}`,
      );
    }
    await page.locator(args.ready_selector).first().waitFor({
      state: "visible",
      timeout: args.timeout_ms,
    });
    if (authenticatedSelector) {
      await page.locator(authenticatedSelector).first().waitFor({
        state: "visible",
        timeout: args.timeout_ms,
      });
    }
    const finalUrl = page.url();
    assertExpectedFinalUrl(finalUrl, expectedUrl);
    const measurement = await page.evaluate((rowSelector) => {
      const memory = performance.memory;
      return {
        domNodes: document.getElementsByTagName("*").length,
        renderedRows: document.querySelectorAll(rowSelector).length,
        heapBytes:
          memory && Number.isFinite(memory.usedJSHeapSize)
            ? memory.usedJSHeapSize
            : null,
      };
    }, args.row_selector);
    const parsedFinalUrl = new URL(finalUrl);
    const dimensions = {
      route: normalizedPathname(parsedFinalUrl.pathname),
      final_url: sanitizedPageUrl(finalUrl),
      viewport: args.viewport,
    };
    const records = [
      metric(
        args,
        "frontend.dom_nodes",
        measurement.domNodes,
        "count",
        dimensions,
      ),
      metric(
        args,
        "frontend.rendered_rows",
        measurement.renderedRows,
        "count",
        dimensions,
      ),
    ];
    if (measurement.heapBytes !== null) {
      records.push(
        metric(
          args,
          "frontend.heap_bytes",
          measurement.heapBytes,
          "bytes",
          dimensions,
        ),
      );
    }
    writeJsonl(args.output, records, args.lock_timeout_ms);
  } finally {
    if (context) await context.close();
    await browser.close();
  }
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : "";
if (invokedPath === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(`DOM capture failed: ${error.message}${os.EOL}`);
    process.exitCode = 2;
  });
}
