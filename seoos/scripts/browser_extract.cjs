'use strict';

const net = require('node:net');
const readline = require('node:readline');
const { once } = require('node:events');

const input = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
const pending = new Map();
let proxy;
let browser;
let nextId = 0;
let stopped = false;

function send(value) {
  return new Promise((resolve, reject) => {
    process.stdout.write(JSON.stringify(value) + '\n', error => error ? reject(error) : resolve());
  });
}

async function close() {
  if (stopped) return;
  stopped = true;
  input.close();
  for (const resolve of pending.values()) resolve(null);
  pending.clear();
  try {
    await browser?.close();
  } finally {
    if (proxy?.listening) proxy.close();
  }
}

process.once('SIGTERM', () => void close());
process.stdout.on('error', () => void close());

async function run() {
  const [line] = await once(input, 'line');
  const { url } = JSON.parse(line);
  input.on('line', value => {
    const response = JSON.parse(value);
    const resolve = pending.get(response.id);
    if (resolve) {
      pending.delete(response.id);
      resolve(response);
    }
  });
  input.once('close', () => void close());

  try {
    const { chromium } = require(process.env.SEOOS_PLAYWRIGHT_MODULE || 'playwright');
    // The proxy deliberately has no outbound client. Traffic not intercepted
    // by Playwright has no fallback network path, including on loopback.
    proxy = net.createServer(socket => socket.destroy());
    proxy.listen(0, '127.0.0.1');
    await once(proxy, 'listening');
    browser = await chromium.launch({
      channel: process.env.SEOOS_PLAYWRIGHT_CHANNEL || undefined,
      executablePath: process.env.SEOOS_CHROMIUM_EXECUTABLE || undefined,
      chromiumSandbox: true,
      headless: true,
      timeout: 8_000,
      proxy: { server: 'http://127.0.0.1:' + proxy.address().port, bypass: '<-loopback>' },
      args: [
        '--disable-quic',
        '--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1',
        '--force-webrtc-ip-handling-policy=disable_non_proxied_udp',
        '--no-pings',
      ],
    });
  } catch {
    await send({ type: 'unavailable' });
    return;
  }

  const context = await browser.newContext({ serviceWorkers: 'block', acceptDownloads: false });
  const resources = new Set(['document', 'script', 'stylesheet', 'fetch', 'xhr']);
  let capped = false;
  await context.routeWebSocket('**/*', socket => socket.close());
  await context.route('**/*', async route => {
    const request = route.request();
    if (stopped || !resources.has(request.resourceType()) || request.method() !== 'GET') {
      await route.abort().catch(() => {});
      return;
    }
    if (nextId >= 40) {
      capped = true;
      await route.abort().catch(() => {});
      return;
    }
    const id = ++nextId;
    const reply = new Promise(resolve => pending.set(id, resolve));
    try {
      await send({ type: 'request', id, url: request.url(), method: request.method(), resource: request.resourceType() });
      const response = await reply;
      if (response?.ok) {
        await route.fulfill({ status: response.status, headers: response.headers, body: Buffer.from(response.body || '', 'base64') });
      } else {
        await route.abort();
      }
    } catch {
      await route.abort().catch(() => {});
    } finally {
      pending.delete(id);
    }
  });
  await context.addInitScript(() => {
    for (const name of ['RTCPeerConnection', 'webkitRTCPeerConnection', 'WebTransport']) {
      Object.defineProperty(globalThis, name, { value: undefined, configurable: false });
    }
  });
  const page = await context.newPage();
  context.on('page', other => {
    if (other !== page) void other.close();
  });
  const response = await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 12_000 });
  if (!response?.ok()) throw new Error('Navigation failed');
  await page.waitForLoadState('networkidle', { timeout: 1800 }).catch(() => { capped = true; });
  const result = await page.evaluate(() => {
    const content = document.querySelector('main,[role="main"]') || document.querySelector('article') || document.body;
    const body = content?.innerText || '';
    const headings = [...(content?.querySelectorAll('h1,h2,h3,h4,h5,h6') || [])];
    const visible = headings.filter(heading => heading.getClientRects().length > 0 && getComputedStyle(heading).visibility === 'visible');
    return {
      title: document.title.slice(0, 1024),
      text: body.slice(0, 120_000),
      headings: visible.slice(0, 100).map(heading => heading.tagName + ' ' + heading.innerText.slice(0, 256)),
      truncated: body.length > 120_000 || visible.length > 100,
    };
  });
  await send({ ...result, type: 'result', url: page.url(), truncated: capped || result.truncated });
}

run().catch(() => send({ type: 'failed' }).catch(() => {})).finally(close);
