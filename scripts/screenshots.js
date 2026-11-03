// Retake the README screenshots (docs/images/*.png) of the web board against a running demo server.
//
//   scripts/demo-repos.sh /tmp/demo
//   relai-server init --scan /tmp/demo --dir /tmp/demo-board --members alice,bob,agent-claude-1,agent-claude-2 --listen 127.0.0.1:18160
//   (set "refresh_interval": "2s" in /tmp/demo-board/config.json, start relai-server, note the four tokens)
//   npm i playwright-core   # in a scratch folder, not in this repo
//   OUT=docs/images URL=http://127.0.0.1:18160 REPO_DIR=/tmp/demo \
//     TOKENS='{"alice":"rt_…","bob":"rt_…","agent-claude-1":"rt_…","agent-claude-2":"rt_…"}' node scripts/screenshots.js
//
// Set PLAYWRIGHT_CHROMIUM to a chromium binary if it is not under ~/.cache/ms-playwright.
// The script claims and releases items and runs `bd update` in REPO_DIR so the activity feed is real.
const { chromium } = require('playwright-core');
const { execSync } = require('child_process');
const OUT = process.env.OUT;
const URL = process.env.URL;
const TOK = JSON.parse(process.env.TOKENS); // {identity: token}
const REPO_DIR = process.env.REPO_DIR;

const sleep = ms => new Promise(r => setTimeout(r, ms));

(async () => {
  const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM || undefined, headless: true, args: ['--no-sandbox'] });

  async function open(colorScheme) {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 860 }, colorScheme, deviceScaleFactor: 1 });
    const page = await ctx.newPage();
    await page.goto(URL + '/board');
    return page;
  }
  const api = (page, who, method, path) => page.evaluate(async ([t, m, p]) => {
    const r = await fetch(p, { method: m, headers: { Authorization: 'Bearer ' + t } });
    return r.status;
  }, [TOK[who], method, path]);
  const items = page => page.evaluate(async t => (await fetch('/items', { headers: { Authorization: 'Bearer ' + t } })).json(), TOK.alice);
  const claim = async (page, who, repo, titleStart) => {
    const it = (await items(page)).find(i => i.repo === repo && i.title.startsWith(titleStart));
    if (!it) throw new Error('no item ' + repo + ' ' + titleStart);
    const st = await api(page, who, 'POST', '/items/' + encodeURIComponent(it.id) + '/claim');
    if (st !== 200) throw new Error('claim ' + st);
    return it;
  };

  // 1. login screen
  const login = await open('light');
  await login.waitForSelector('#login');
  await login.screenshot({ path: OUT + '/login.png' });
  await login.context().close();

  // 2. the board, with a team working on it
  const page = await open('light');
  await page.fill('#tok', TOK.alice); await page.click('button.primary');
  await page.waitForSelector('.card');
  await page.waitForFunction(() => document.getElementById('connt').textContent === 'live');

  const a = await claim(page, 'alice', 'auth-service', 'Rotate signing keys'); await sleep(400);
  await claim(page, 'agent-claude-1', 'billing-api', 'Handle partial refunds'); await sleep(400);
  await claim(page, 'agent-claude-1', 'billing-api', 'Idempotency keys'); await sleep(400);
  await claim(page, 'agent-claude-2', 'web-frontend', 'Lazy-load'); await sleep(400);
  await claim(page, 'bob', 'design-system', 'Button focus ring'); await sleep(400);
  const r = await claim(page, 'agent-claude-2', 'auth-service', 'Rate-limit'); await sleep(400);
  await api(page, 'agent-claude-2', 'POST', '/items/' + encodeURIComponent(r.id) + '/release'); await sleep(400);
  await claim(page, 'bob', 'auth-service', 'Rate-limit'); await sleep(400);
  // a change made in a repo, outside the board, as a developer would with bd
  const id = JSON.parse(execSync('bd list --json', { cwd: REPO_DIR + '/data-pipeline' }).toString()).find(t => t.title.startsWith('Dedupe')).id;
  execSync(`bd update ${id} -s in_progress`, { cwd: REPO_DIR + '/data-pipeline', stdio: 'ignore' });
  await page.waitForFunction(() => [...document.querySelectorAll('#feed li')].some(li => li.textContent.includes('in progress')), null, { timeout: 15000 });
  await sleep(1200); // let the highlight fade
  await page.screenshot({ path: OUT + '/board.png' });

  // 3. one person's view, and dark mode
  await page.click('.chip[data-f="who"][data-v="agent-claude-1"]'); await sleep(300);
  await page.screenshot({ path: OUT + '/board-filtered.png' });
  await page.context().close();

  const dark = await open('dark');
  await dark.fill('#tok', TOK.bob); await dark.click('button.primary');
  await dark.waitForSelector('.card');
  await dark.waitForFunction(() => document.getElementById('connt').textContent === 'live');
  await sleep(600);
  await dark.screenshot({ path: OUT + '/board-dark.png' });
  await browser.close();
  console.log('ok');
})().catch(e => { console.error('FAILED', e); process.exit(1); });
