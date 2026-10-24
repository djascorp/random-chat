#!/usr/bin/env node
/**
 * Layout regression check.
 *
 * Drives two real Chrome instances through the DevTools protocol, forces a real
 * match between them with a synthetic camera, then measures the layout at every
 * viewport that matters. It exists because eyeballing a screenshot is not a test:
 * the first version of this UI wrapped its control bar onto three rows at
 * 1180x700 (a `left: 50%` box with no width is capped at half the viewport),
 * which no amount of staring at the code revealed.
 *
 * Usage:
 *   1. start the API:    make dev-api        (or ./bin/server)
 *   2. run this script:  npm run check:layout
 *
 * Screenshots are written to web/.layout-shots/ for a human to look at.
 * Set CHROME=/path/to/chrome to override the browser discovery.
 */
import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB_ROOT = join(HERE, '..');
const SHOTS = join(WEB_ROOT, '.layout-shots');
const APP = process.env.APP_URL ?? 'http://localhost:8080';
const PROFILES = join(WEB_ROOT, '.layout-profiles');

const CHROME_CANDIDATES = [
  process.env.CHROME,
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/Applications/Chromium.app/Contents/MacOS/Chromium',
  '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge',
  '/Applications/Brave Browser.app/Contents/MacOS/Brave Browser',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser',
].filter(Boolean);

const VIEWPORTS = [
  { name: 'laptop-1180x700', w: 1180, h: 700 },
  { name: 'laptop-1440x900', w: 1440, h: 900 },
  { name: 'tablet-834x1112', w: 834, h: 1112 },
  { name: 'phone-390x844', w: 390, h: 844 },
  { name: 'phone-320x568', w: 320, h: 568 },
  { name: 'landscape-740x360', w: 740, h: 360 },
];

const problems = [];
const record = (viewport, check, ok, detail) => {
  if (!ok) problems.push(`${viewport}: ${check} -> ${detail}`);
  return ok;
};
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Best-effort directory removal. Chrome keeps files open in its profile for a
 * moment after being signalled, and a failing cleanup must never hide the test
 * result.
 */
function removeDir(path) {
  try {
    rmSync(path, { recursive: true, force: true });
  } catch {
    // Left behind; both directories are gitignored.
  }
}

/** Minimal DevTools protocol client over the target WebSocket. */
class CDP {
  constructor(socket) {
    this.socket = socket;
    this.nextId = 0;
    this.pending = new Map();
    socket.addEventListener('message', (event) => {
      const message = JSON.parse(event.data);
      const waiter = message.id ? this.pending.get(message.id) : undefined;
      if (!waiter) return;
      this.pending.delete(message.id);
      if (message.error) waiter.reject(new Error(JSON.stringify(message.error)));
      else waiter.resolve(message.result);
    });
  }

  send(method, params = {}) {
    const id = ++this.nextId;
    this.socket.send(JSON.stringify({ id, method, params }));
    return new Promise((resolve, reject) => this.pending.set(id, { resolve, reject }));
  }

  async eval(expression) {
    const result = await this.send('Runtime.evaluate', {
      expression,
      returnByValue: true,
      awaitPromise: true,
    });
    if (result.exceptionDetails) throw new Error(result.exceptionDetails.text ?? 'evaluate failed');
    return result.result.value;
  }

  async screenshot(name) {
    const { data } = await this.send('Page.captureScreenshot', { format: 'png' });
    writeFileSync(join(SHOTS, `${name}.png`), Buffer.from(data, 'base64'));
  }
}

async function connect(port) {
  for (let attempt = 0; attempt < 60; attempt += 1) {
    try {
      const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
      const page = targets.find((t) => t.type === 'page' && t.url.startsWith(APP));
      if (page?.webSocketDebuggerUrl) {
        const socket = new WebSocket(page.webSocketDebuggerUrl);
        await new Promise((resolve, reject) => {
          socket.addEventListener('open', resolve, { once: true });
          socket.addEventListener('error', reject, { once: true });
        });
        return new CDP(socket);
      }
    } catch {
      // Chrome is not listening yet.
    }
    await sleep(250);
  }
  throw new Error(`no debuggable page on port ${port} — is ${APP} running?`);
}

async function waitFor(client, expression, { timeout = 20000, label = expression } = {}) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (await client.eval(`!!(${expression})`)) return;
    await sleep(200);
  }
  throw new Error(`timed out waiting for ${label}`);
}

// Runs inside the page. Every figure here is a layout fact, not a guess.
const MEASURE = `(() => {
  const box = (sel) => {
    const el = document.querySelector(sel);
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { top: Math.round(r.top), bottom: Math.round(r.bottom), left: Math.round(r.left), right: Math.round(r.right), w: Math.round(r.width), h: Math.round(r.height) };
  };
  const bar = document.querySelector('.controls');
  const controls = bar ? [...bar.querySelectorAll('.ctl')] : [];
  const tops = controls.map((el) => el.getBoundingClientRect().top);
  const minTop = tops.length ? Math.min(...tops) : 0;
  // Buttons differ by a pixel or two in height, so group by proximity rather
  // than comparing rounded values.
  const firstRowItems = tops.filter((top) => Math.abs(top - minTop) <= 3).length;
  const visibleLabels = controls.filter((el) => {
    const label = el.querySelector('.ctl__label');
    return label && getComputedStyle(label).display !== 'none';
  }).length;
  const b = box('.controls');
  const remote = box('.stage__remote');
  // The frame is shaped to the camera, so the frame's box no longer says how
  // much picture is on screen: contain may letterbox it. Work out the
  // painted rectangle from the decoded size instead.
  const video = document.querySelector('.stage__remote video');
  const streamAspect = video && video.videoWidth > 0 && video.videoHeight > 0
    ? video.videoWidth / video.videoHeight
    : null;
  let painted = null;
  if (remote) {
    painted = { w: remote.w, h: remote.h };
    if (streamAspect) {
      if (remote.w / remote.h > streamAspect) {
        painted.w = Math.round(remote.h * streamAspect);
      } else {
        painted.h = Math.round(remote.w / streamAspect);
      }
    }
  }
  const composer = box('.chat__composer');
  const local = box('.stage__local');
  const doc = document.documentElement;

  // What is painted at a point tells us whether something covers it.
  const topmostAt = (rect, selector) => {
    if (!rect) return null;
    const hit = document.elementFromPoint((rect.left + rect.right) / 2, (rect.top + rect.bottom) / 2);
    return hit ? !!hit.closest(selector) : false;
  };

  return JSON.stringify({
    viewport: { w: innerWidth, h: innerHeight },
    pageScrollPx: doc.scrollHeight - doc.clientHeight,
    controlsHeightVar: getComputedStyle(doc).getPropertyValue('--controls-height').trim(),
    firstRowItems,
    controlsTotal: controls.length,
    controlsWithLabel: visibleLabels,
    barHeight: b ? b.h : null,
    bar: b,
    remote,
    painted,
    streamAspect,
    frameAspect: remote && remote.h > 0 ? remote.w / remote.h : null,
    local,
    chatOpen: document.querySelector('.chat')?.classList.contains('is-open') ?? false,
    composer,
    composerTopmost: topmostAt(composer, '.chat__composer'),
    localTopmost: topmostAt(local, '.stage__local'),
    barCoversVideo: b && remote ? b.top < remote.bottom && b.bottom > remote.top && b.left < remote.right && b.right > remote.left : null,
    composerInViewport: composer ? composer.bottom <= innerHeight + 1 : null,
    localInsideVideo: local && remote ? local.right <= remote.right + 1 && local.bottom <= remote.bottom + 1 : null,
    // 150px is the honest floor: the narrowest viewport can only show a
    // landscape camera at ~162px tall once it fills the width.
    videoUsable: painted ? painted.h >= 150 : null,
  });
})()`;

function launch(port, profile) {
  const chrome = CHROME_CANDIDATES.find((candidate) => existsSync(candidate));
  if (!chrome) {
    throw new Error(`no Chrome found. Set CHROME=/path/to/chrome. Tried:\n  ${CHROME_CANDIDATES.join('\n  ')}`);
  }
  const child = spawn(
    chrome,
    [
      '--headless=new',
      '--no-first-run',
      '--no-default-browser-check',
      '--disable-gpu',
      '--autoplay-policy=no-user-gesture-required',
      '--use-fake-device-for-media-stream',
      '--use-fake-ui-for-media-stream',
      '--window-size=1180,700',
      `--user-data-dir=${profile}`,
      `--remote-debugging-port=${port}`,
      APP,
    ],
    { stdio: 'ignore', detached: false },
  );
  return child;
}

const launchBrowser = () => {
  removeDir(PROFILES);
  mkdirSync(SHOTS, { recursive: true });
  return [launch(9222, join(PROFILES, 'a')), launch(9223, join(PROFILES, 'b'))];
};

const browsers = launchBrowser();
const shutdown = () => {
  for (const child of browsers) {
    try {
      child.kill('SIGTERM');
    } catch {
      // already gone
    }
  }
};

try {
  const a = await connect(9222);
  const b = await connect(9223);
  for (const client of [a, b]) {
    await client.send('Page.enable');
    await client.send('Runtime.enable');
  }

  // Pre-accept the age gate, then reload so the landing is interactive.
  for (const client of [a, b]) {
    await waitFor(client, `document.readyState === 'complete'`, { label: 'initial page load' });
    await client.eval(`localStorage.setItem('random-chat.consent-v1','true')`);
    await client.send('Page.reload');
    await waitFor(client, `document.readyState === 'complete'`, { label: 'page reload' });
    await sleep(500);
  }

  const startButton = `[...document.querySelectorAll('button')].find((el) => /Démarrer|Start/i.test(el.textContent))`;
  for (const client of [a, b]) {
    await waitFor(client, `${startButton} !== undefined`, { label: 'start button visible' });
    await client.eval(`${startButton}.click()`);
  }

  await waitFor(a, `document.querySelector('.stage__remote.is-live')`, { label: 'peer A to reach a live call' });
  await waitFor(b, `document.querySelector('.stage__remote.is-live')`, { label: 'peer B to reach a live call' });
  console.log('live call established between the two instances');

  for (const viewport of VIEWPORTS) {
    await a.send('Emulation.setDeviceMetricsOverride', {
      width: viewport.w,
      height: viewport.h,
      deviceScaleFactor: 1,
      mobile: viewport.w < 500,
    });
    await sleep(450);

    const m = JSON.parse(await a.eval(MEASURE));
    console.log(`${viewport.name}  bar=${m.bar?.w}x${m.barHeight} rows=${m.firstRowItems}/${m.controlsTotal} labels=${m.controlsWithLabel} scroll=${m.pageScrollPx}px frame=${m.remote?.w}x${m.remote?.h} picture=${m.painted?.w}x${m.painted?.h}`);

    record(viewport.name, 'page does not scroll', m.pageScrollPx <= 1, `${m.pageScrollPx}px of overflow`);
    record(viewport.name, 'control bar does not cover the video', m.barCoversVideo === false, String(m.barCoversVideo));
    record(viewport.name, 'video keeps a usable height', m.videoUsable === true, `${m.painted?.h}px`);
    // The point of the shaped frame: no letterboxing means the frame's ratio
    // is the camera's ratio.
    record(
      viewport.name,
      'frame matches the camera aspect',
      m.streamAspect === null || Math.abs(m.frameAspect - m.streamAspect) < 0.02,
      `frame=${m.frameAspect?.toFixed(3)} camera=${m.streamAspect?.toFixed(3)}`,
    );
    record(viewport.name, 'self view stays inside the video', m.localInsideVideo === true, String(m.localInsideVideo));
    record(viewport.name, 'self view is painted above the video', m.localTopmost === true, String(m.localTopmost));
    record(
      viewport.name,
      'reserved height matches the measured bar',
      m.controlsHeightVar === `${m.barHeight}px`,
      `var=${m.controlsHeightVar} measured=${m.barHeight}px`,
    );
    // Phones legitimately wrap the nine actions; from tablet width up, one row.
    if (viewport.w >= 820) {
      record(
        viewport.name,
        'control bar on a single row',
        m.firstRowItems === m.controlsTotal,
        `${m.firstRowItems}/${m.controlsTotal} on the first row`,
      );
    }

    await a.screenshot(`call-${viewport.name}`);
  }

  // Chat panel on a phone (bottom sheet) and a laptop (side panel).
  for (const viewport of [
    { name: 'phone-390x844', w: 390, h: 844 },
    { name: 'laptop-1180x700', w: 1180, h: 700 },
  ]) {
    await a.send('Emulation.setDeviceMetricsOverride', {
      width: viewport.w,
      height: viewport.h,
      deviceScaleFactor: 1,
      mobile: viewport.w < 500,
    });
    await sleep(300);
    await a.eval(`[...document.querySelectorAll('.ctl')].find((el) => /chat/i.test(el.title)).click()`);
    await sleep(600);

    const m = JSON.parse(await a.eval(MEASURE));
    console.log(`${viewport.name} + chat  composer=${m.composer?.bottom}px topmost=${m.composerTopmost}`);

    record(viewport.name, 'chat opens', m.chatOpen === true, String(m.chatOpen));
    record(viewport.name, 'chat composer stays on screen', m.composerInViewport === true, `${m.composer?.bottom} vs ${viewport.h}`);
    record(viewport.name, 'nothing covers the chat composer', m.composerTopmost === true, String(m.composerTopmost));

    await a.screenshot(`chat-${viewport.name}`);
    await a.eval(`document.querySelector('.chat .icon-btn').click()`);
    await sleep(400);
  }

  // Share dialog on a laptop, opened from the live call bar: the link is
  // minted for real, so this covers the dialog, the readonly URL row and
  // the cap stepper in one shot.
  await a.send('Emulation.setDeviceMetricsOverride', { width: 1180, height: 700, deviceScaleFactor: 1, mobile: false });
  await sleep(300);
  await a.eval(`[...document.querySelectorAll('.ctl')].find((el) => (el.getAttribute('aria-label') || '').startsWith('Invit'))?.click()`);
  await waitFor(a, `document.querySelector('.share-row__input')?.value?.length > 0`, { label: 'share dialog to mint a link' });
  await sleep(400);
  const share = JSON.parse(
    await a.eval(`(() => {
      const modal = document.querySelector('.modal');
      const r = modal ? modal.getBoundingClientRect() : null;
      return JSON.stringify({
        open: !!modal,
        width: r ? Math.round(r.width) : null,
        fits: r ? r.top >= 0 && r.bottom <= innerHeight + 1 : null,
      });
    })()`),
  );
  console.log(`share-1180x700  modal=${share.width}px fits=${share.fits}`);
  record('share-1180x700', 'dialog opens with a minted link', share.open === true, String(share.open));
  record('share-1180x700', 'dialog fits the viewport', share.fits === true, `width=${share.width}px`);
  await a.screenshot('share-laptop-1180x700');
  await a.eval(`document.querySelector('.modal .icon-btn')?.click()`);
  await sleep(400);

  // Invitation screen on a phone, with a dead link: needs no live session,
  // and the dead template shares the hero layout with the ready one.
  await b.send('Page.navigate', { url: `${APP}/j/0123456789abcdef0123456789abcdef` });
  await waitFor(
    b,
    `document.body.textContent.includes('ne fonctionne plus') || document.body.textContent.includes('no longer works')`,
    { label: 'join screen to show the dead link' },
  );
  await b.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  await sleep(500);
  const join = JSON.parse(
    await b.eval(`(() => JSON.stringify({
      scroll: document.documentElement.scrollHeight - document.documentElement.clientHeight,
    }))()`),
  );
  console.log(`join-390x844  scroll=${join.scroll}px`);
  record('join-390x844', 'dead-link screen does not scroll', join.scroll <= 1, `${join.scroll}px of overflow`);
  await b.screenshot('join-phone-390x844');

  // Landing page on a phone.
  await a.eval(`document.querySelector('.ctl--danger').click()`);
  await sleep(600);
  await a.send('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true });
  await sleep(500);

  const landing = JSON.parse(
    await a.eval(`(() => {
      const doc = document.documentElement;
      const metrics = document.querySelector('.metrics');
      const card = document.querySelector('.metric');
      return JSON.stringify({
        metricsOverflowPx: metrics ? metrics.scrollWidth - metrics.clientWidth : null,
        metricWidth: card ? Math.round(card.getBoundingClientRect().width) : null,
      });
    })()`),
  );
  console.log(`landing-390x844  metricCard=${landing.metricWidth}px overflow=${landing.metricsOverflowPx}px`);
  record('landing-390x844', 'metrics grid does not overflow', landing.metricsOverflowPx === 0, `${landing.metricsOverflowPx}px`);
  record('landing-390x844', 'metric cards stay readable', landing.metricWidth >= 140, `${landing.metricWidth}px`);
  await a.screenshot('landing-phone-390x844');
} finally {
  shutdown();
  await sleep(400);
  removeDir(PROFILES);
}

if (problems.length === 0) {
  console.log(`\nALL LAYOUT CHECKS PASSED — screenshots in ${SHOTS}`);
  process.exit(0);
}
console.log(`\n${problems.length} LAYOUT PROBLEM(S):`);
for (const problem of problems) console.log(`  - ${problem}`);
console.log(`\nscreenshots in ${SHOTS}`);
process.exit(1);
