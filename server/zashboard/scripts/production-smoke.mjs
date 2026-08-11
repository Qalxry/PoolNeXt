#!/usr/bin/env node

import { spawn } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { mkdtemp, rm } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDir = dirname(fileURLToPath(import.meta.url))
const requireFromWeb = createRequire(resolve(scriptDir, '../../web/package.json'))
const { chromium, devices } = requireFromWeb('@playwright/test')

const args = process.argv.slice(2)
const binaryIndex = args.indexOf('--binary')
if (binaryIndex === -1 || !args[binaryIndex + 1]) {
  throw new Error('usage: production-smoke.mjs --binary /path/to/poolnext-server')
}

const binaryPath = resolve(args[binaryIndex + 1])
const dataDir = await mkdtemp(resolve(tmpdir(), 'poolnext-production-smoke-'))
const sqlitePath = resolve(dataDir, 'poolnext.db')
const uploadPath = resolve(dataDir, 'upload')
const port = await reservePort()
const baseURL = `http://127.0.0.1:${port}`
const serverOutput = []

const server = spawn(binaryPath, [], {
  cwd: dataDir,
  env: {
    ...process.env,
    GIN_MODE: 'release',
    LOG_LEVEL: 'warn',
    PORT: String(port),
    SESSION_SECRET: randomBytes(32).toString('hex'),
    SQLITE_PATH: sqlitePath,
    UPLOAD_PATH: uploadPath,
  },
  stdio: ['ignore', 'pipe', 'pipe'],
})

for (const stream of [server.stdout, server.stderr]) {
  stream.setEncoding('utf8')
  stream.on('data', (chunk) => {
    serverOutput.push(chunk)
    if (serverOutput.length > 200) serverOutput.shift()
  })
}

let browser
try {
  await waitForReady(`${baseURL}/api/status`, server)
  const bootstrapPassword = /一次性密码:\s*(.+)$/m.exec(serverOutput.join(''))?.[1]?.trim()
  if (!bootstrapPassword) throw new Error('bootstrap password was not printed to server stdout')

  browser = await chromium.launch({ headless: true })
  const changedPassword = `Smoke-${randomBytes(5).toString('hex')}`
  const profiles = [
    ['desktop', devices['Desktop Chrome']],
    ['mobile', devices['iPhone 13']],
  ]

  for (const [name, device] of profiles) {
    const context = await browser.newContext({
      baseURL,
      deviceScaleFactor: device.deviceScaleFactor,
      hasTouch: device.hasTouch,
      isMobile: device.isMobile,
      serviceWorkers: 'block',
      userAgent: device.userAgent,
      viewport: device.viewport,
    })
    try {
      const password = name === 'desktop' ? bootstrapPassword : changedPassword
      await login(context, baseURL, password)
      if (name === 'desktop') {
        await changeBootstrapPassword(context, baseURL, changedPassword)
      }
      await smokeProfile(context, baseURL, name)
    } finally {
      await context.close()
    }
  }

  process.stdout.write(
    'Production browser smoke passed for desktop, mobile, WebUI chunks, and Zashboard.\n',
  )
} catch (error) {
  const output = serverOutput.join('').trim()
  if (output) process.stderr.write(`\nPoolNeXt output:\n${output}\n`)
  throw error
} finally {
  if (browser) await browser.close()
  await stopProcess(server)
  await rm(dataDir, { recursive: true, force: true })
}

async function reservePort() {
  const listener = createServer()
  await new Promise((resolvePromise, rejectPromise) => {
    listener.once('error', rejectPromise)
    listener.listen(0, '127.0.0.1', resolvePromise)
  })
  const address = listener.address()
  const selectedPort = typeof address === 'object' && address ? address.port : 0
  await new Promise((resolvePromise, rejectPromise) => {
    listener.close((error) => (error ? rejectPromise(error) : resolvePromise()))
  })
  if (!selectedPort) throw new Error('failed to reserve a local port')
  return selectedPort
}

async function waitForReady(url, processHandle) {
  for (let attempt = 1; attempt <= 60; attempt += 1) {
    if (processHandle.exitCode !== null) {
      throw new Error(`PoolNeXt exited before becoming ready with code ${processHandle.exitCode}`)
    }
    try {
      const response = await fetch(url)
      if (response.ok) return
    } catch {
      // Startup races are expected until the listener is ready.
    }
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 500))
  }
  throw new Error(`PoolNeXt did not become ready at ${url}`)
}

async function login(context, baseURL, password) {
  const response = await context.request.post(`${baseURL}/api/user/login`, {
    data: { username: 'root', password },
  })
  const payload = await response.json()
  if (!response.ok() || payload.success !== true) {
    throw new Error(`root login failed: ${JSON.stringify(payload)}`)
  }
}

async function changeBootstrapPassword(context, baseURL, password) {
  const response = await context.request.post(`${baseURL}/api/user/self/update`, {
    data: {
      username: 'root',
      display_name: 'root',
      password,
    },
  })
  const payload = await response.json()
  if (!response.ok() || payload.success !== true) {
    throw new Error(`bootstrap password change failed: ${JSON.stringify(payload)}`)
  }
}

async function smokeProfile(context, baseURL, profileName) {
  await context.addInitScript(() => {
    localStorage.setItem('config/auto-upgrade', 'false')
    localStorage.setItem('config/check-upgrade-core', 'false')
    const nativeFetch = window.fetch.bind(window)
    window.fetch = (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
      if (url.startsWith('https://api.github.com/')) {
        return Promise.resolve(
          new Response(JSON.stringify({ tag_name: 'v1.19.24', assets: [] }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        )
      }
      return nativeFetch(input, init)
    }
  })
  const page = await context.newPage()
  const consoleErrors = []
  const pageErrors = []
  const staticFailures = []
  const apiFailures = []
  const loadedStaticAssets = new Set()

  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => pageErrors.push(error.message))
  page.on('requestfailed', (request) => {
    if (isStaticAsset(request.url())) {
      staticFailures.push(`${request.url()}: ${request.failure()?.errorText || 'request failed'}`)
    }
  })
  page.on('response', (response) => {
    const pathname = new URL(response.url()).pathname
    if (pathname.startsWith('/api/') && response.status() >= 400) {
      apiFailures.push(`${response.request().method()} ${pathname}: HTTP ${response.status()}`)
    }
    if (isStaticAsset(response.url())) {
      loadedStaticAssets.add(pathname)
      if (response.status() >= 400) {
        staticFailures.push(`${response.url()}: HTTP ${response.status()}`)
      }
    }
  })

  await page.route('**/api/zashboard/clash/**', async (route) => {
    const pathname = new URL(route.request().url()).pathname.replace('/api/zashboard/clash', '')
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify(zashboardFixture(pathname)),
    })
  })
  if (typeof page.routeWebSocket === 'function') {
    await page.routeWebSocket('**/api/zashboard/clash/connections**', (webSocket) => {
      webSocket.send(
        JSON.stringify({ connections: [], downloadTotal: 0, uploadTotal: 0, memory: 0 }),
      )
    })
    await page.routeWebSocket('**/api/zashboard/clash/logs**', (webSocket) => {
      webSocket.send(JSON.stringify({ type: 'info', payload: 'production smoke' }))
    })
    await page.routeWebSocket('**/api/zashboard/clash/memory**', (webSocket) => {
      webSocket.send(JSON.stringify({ inuse: 0 }))
    })
    await page.routeWebSocket('**/api/zashboard/clash/traffic**', (webSocket) => {
      webSocket.send(JSON.stringify({ down: 0, up: 0 }))
    })
  }

  const loginResponse = await page.goto(`${baseURL}/login`, { waitUntil: 'networkidle' })
  if (!loginResponse?.ok()) throw new Error(`${profileName}: WebUI login page failed to load`)
  await page.locator('body').waitFor({ state: 'visible' })
  if ((await page.locator('body').innerText()).trim().length === 0) {
    throw new Error(`${profileName}: WebUI rendered an empty body`)
  }
  if (
    ![...loadedStaticAssets].some(
      (path) => path.startsWith('/_next/static/') && path.endsWith('.js'),
    )
  ) {
    throw new Error(`${profileName}: WebUI did not load a Next.js JavaScript chunk`)
  }

  const affectedRoutes = [
    ['/nodes', '节点池'],
    ['/workspace', '编排'],
    ['/runtime', '内核'],
    ['/log', '事件日志'],
    ['/setting', '设置'],
  ]
  for (const [pathname, heading] of affectedRoutes) {
    const response = await page.goto(`${baseURL}${pathname}`, { waitUntil: 'networkidle' })
    if (!response?.ok()) {
      const status = response?.status() ?? 'no response'
      const bodyText = (await page.locator('body').innerText()).replace(/\s+/g, ' ').trim()
      throw new Error(
        [
          `${profileName}: ${pathname} failed to load with ${status}`,
          `current URL: ${page.url()}`,
          `body: ${bodyText.slice(0, 1000) || '<empty>'}`,
          apiFailures.length > 0 ? `API failures: ${apiFailures.join('; ')}` : '',
        ]
          .filter(Boolean)
          .join('\n'),
      )
    }
    try {
      await page.getByRole('heading', { level: 1, name: heading }).waitFor({ state: 'visible' })
    } catch (error) {
      const bodyText = (await page.locator('body').innerText()).replace(/\s+/g, ' ').trim()
      const diagnostics = [
        `${profileName}: ${pathname} did not render heading ${JSON.stringify(heading)}`,
        `current URL: ${page.url()}`,
        `body: ${bodyText.slice(0, 1000) || '<empty>'}`,
      ]
      if (apiFailures.length > 0) diagnostics.push(`API failures: ${apiFailures.join('; ')}`)
      if (consoleErrors.length > 0) {
        diagnostics.push(`console errors: ${consoleErrors.join('; ')}`)
      }
      if (pageErrors.length > 0) diagnostics.push(`page errors: ${pageErrors.join('; ')}`)
      throw new Error(diagnostics.join('\n'), { cause: error })
    }
  }

  const zashboardResponse = await page.goto(`${baseURL}/zashboard/`, { waitUntil: 'networkidle' })
  if (!zashboardResponse?.ok()) throw new Error(`${profileName}: Zashboard failed to load`)
  await page.locator('#app > *').first().waitFor({ state: 'visible' })
  if (
    ![...loadedStaticAssets].some(
      (path) => path.startsWith('/zashboard/assets/') && path.endsWith('.js'),
    )
  ) {
    throw new Error(`${profileName}: Zashboard did not load its JavaScript bundle`)
  }

  if (staticFailures.length > 0) {
    throw new Error(`${profileName}: static asset failures:\n${staticFailures.join('\n')}`)
  }
  if (consoleErrors.length > 0) {
    throw new Error(`${profileName}: browser console errors:\n${consoleErrors.join('\n')}`)
  }
  if (pageErrors.length > 0) {
    throw new Error(`${profileName}: uncaught page errors:\n${pageErrors.join('\n')}`)
  }
}

function isStaticAsset(rawURL) {
  const pathname = new URL(rawURL).pathname
  return pathname.startsWith('/_next/static/') || pathname.startsWith('/zashboard/assets/')
}

function zashboardFixture(pathname) {
  switch (pathname) {
    case '/version':
      return { version: 'Mihomo Meta v1.19.24' }
    case '/configs':
      return {
        port: 0,
        'socks-port': 0,
        'redir-port': 0,
        'tproxy-port': 0,
        'mixed-port': 0,
        'allow-lan': false,
        'bind-address': '',
        mode: 'rule',
        'mode-list': ['rule'],
        modes: ['rule'],
        'log-level': 'info',
        ipv6: false,
        tun: { enable: false },
      }
    case '/proxies':
      return {
        proxies: {
          DIRECT: { name: 'DIRECT', type: 'Direct', history: [], udp: true },
          GLOBAL: {
            name: 'GLOBAL',
            type: 'Selector',
            now: 'DIRECT',
            all: ['DIRECT'],
            history: [],
            udp: true,
          },
        },
      }
    case '/providers/proxies':
    case '/providers/rules':
      return { providers: {} }
    case '/rules':
      return { rules: [] }
    default:
      return {}
  }
}

async function stopProcess(processHandle) {
  if (processHandle.exitCode !== null) return
  processHandle.kill('SIGTERM')
  const exited = new Promise((resolvePromise) => processHandle.once('exit', resolvePromise))
  const timeout = new Promise((resolvePromise) => setTimeout(resolvePromise, 5000, 'timeout'))
  if ((await Promise.race([exited, timeout])) === 'timeout' && processHandle.exitCode === null) {
    processHandle.kill('SIGKILL')
    await exited
  }
}
