// Offline browser regression for output from cmd/preview-password-reset-email.
// Uses the project's existing Playwright dependency; never contacts SES or AWS.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const modulePaths = [path.join(__dirname, '..', 'frontend')];
let playwrightModule;
try {
  playwrightModule = require.resolve('@playwright/test', { paths: modulePaths });
} catch {
  // Also supports an existing standalone Playwright installation via NODE_PATH.
  playwrightModule = require.resolve('playwright', { paths: modulePaths });
}
const { chromium } = require(playwrightModule);

async function main() {
  const args = process.argv.slice(2);
  const value = flag => args.includes(flag) ? args[args.indexOf(flag) + 1] : undefined;
  assert(value('--preview'), 'provide --preview OUTPUT_DIRECTORY from the Go preview command');
  const root = path.resolve(value('--preview'));
  const reference = value('--reference');
  const output = path.join(root, 'screenshots');
  await fs.mkdir(output, { recursive: true });
  const [production, preview, plain] = await Promise.all(['production.html', 'preview.html', 'plain-text.txt'].map(name => fs.readFile(path.join(root, name), 'utf8')));
  assert.equal(production.replaceAll('cid:vellum-mark@vellum', 'assets/vellum-mark.png'), preview, 'browser copy must only resolve the CID');
  const minutes = plain.match(/expires in (\d+) minutes/)[1];
  assert.equal((production.match(new RegExp(`${minutes} minutes`, 'g')) || []).length, 2, 'dynamic expiry in both HTML locations');
  const expectedURL = 'https://vellum.example/reset-password#token=PREVIEW_ONLY_000000000000000000000000000000';
  const browser = await chromium.launch({ headless: true, ...(process.env.PLAYWRIGHT_CHANNEL ? { channel: process.env.PLAYWRIGHT_CHANNEL } : {}) });
  const external = [];
  const report = { browser: browser.version(), minutes: Number(minutes), cases: [], externalRequests: external };
  try {
    for (const colorScheme of ['dark', 'light']) {
      const context = await browser.newContext({ deviceScaleFactor: 1, colorScheme, locale: 'en-GB' });
      await context.route('**/*', route => {
        if (route.request().url().startsWith('file://')) return route.continue();
        external.push(route.request().url());
        return route.abort();
      });
      const page = await context.newPage();
      for (const [name, width, height] of [['desktop', 1000, 1000], ['mobile', 390, 844], ['narrow', 320, 720]]) {
        await page.setViewportSize({ width, height });
        await page.goto(pathToFileURL(path.join(root, 'preview.html')).href);
        await page.evaluate(async () => { await document.fonts.ready; await Promise.all([...document.images].map(image => image.decode())); });
        assert.equal(await page.locator('h1').innerText(), 'Reset your\npassword.');
        assert.equal(await page.locator('a').count(), 2);
        for (const link of await page.locator('a').all()) assert.equal(await link.getAttribute('href'), expectedURL);
        const layout = await page.evaluate(() => {
          const action = [...document.querySelectorAll('a')].find(a => a.innerText === 'Reset password');
          const heading = document.querySelector('h1');
          const rect = node => { const r = node.getBoundingClientRect(); return { x:r.x, y:r.y, width:r.width, height:r.height }; };
          const buttonStyle = getComputedStyle(action);
          return {
            overflow: document.documentElement.scrollWidth > innerWidth,
            imageLoaded: [...document.images].every(image => image.naturalWidth > 0),
            action: rect(action), heading: rect(heading),
            buttonBackground: buttonStyle.backgroundColor, buttonColor: buttonStyle.color,
            font: buttonStyle.fontFamily, fontSize: getComputedStyle(heading).fontSize,
            footer: document.body.innerText.includes('This is an automated password-reset email.'),
          };
        });
        assert(!layout.overflow && layout.imageLoaded && layout.footer, 'complete layout with local image and no overflow');
        assert.equal(layout.action.height, 52);
        assert.equal(layout.buttonBackground, 'rgb(255, 36, 108)');
        assert.equal(layout.buttonColor, 'rgb(11, 12, 14)');
        assert.equal(layout.fontSize, '38px');
        assert(layout.font.startsWith('Arial'));
        const screenshot = await page.screenshot({ path: path.join(output, `${name}-${colorScheme}.png`), fullPage: true });
        const testCase = { name, colorScheme, width, ...layout };
        if (reference && colorScheme === 'dark') {
          const baseline = await context.newPage();
          await baseline.setViewportSize({ width, height });
          await baseline.goto(pathToFileURL(path.resolve(reference)).href);
          await baseline.evaluate(async () => { await document.fonts.ready; await Promise.all([...document.images].map(image => image.decode())); });
          const referenceImage = await baseline.screenshot({ fullPage: true });
          // Compare real browser pixels without adding an image-diff dependency.
          const difference = await page.evaluate(async ({ actual, approved }) => {
            const load = src => new Promise((resolve, reject) => { const image = new Image(); image.onload = () => resolve(image); image.onerror = reject; image.src = src; });
            const [a, b] = await Promise.all([load(actual), load(approved)]);
            if (a.width !== b.width || a.height !== b.height) return 1;
            const pixels = image => { const canvas = document.createElement('canvas'); canvas.width=image.width; canvas.height=image.height; const context=canvas.getContext('2d'); context.drawImage(image,0,0); return context.getImageData(0,0,image.width,image.height).data; };
            const ap = pixels(a), bp = pixels(b);
            let changed = 0;
            for (let i=0; i<ap.length; i+=4) if (Math.max(Math.abs(ap[i]-bp[i]),Math.abs(ap[i+1]-bp[i+1]),Math.abs(ap[i+2]-bp[i+2])) > 24) changed++;
            return changed / (a.width*a.height);
          }, { actual: `data:image/png;base64,${screenshot.toString('base64')}`, approved: `data:image/png;base64,${referenceImage.toString('base64')}` });
          testCase.approvedPixelDifference = difference;
          assert(difference < 0.01, `approved Option B visual mismatch: ${difference}`);
          await baseline.close();
        }
        report.cases.push(testCase);
      }
      // Image blocking retains the text brand, action, expiry and full fallback URL.
      await page.setViewportSize({ width: 390, height: 844 });
      await page.route('**/*.png', route => route.abort());
      await page.goto(pathToFileURL(path.join(root, 'preview.html')).href);
      const body = await page.innerText('body');
      for (const text of ['Vellum', 'Reset password', `${minutes} minutes`, 'Single use', expectedURL, 'Your password has not changed.']) assert(body.includes(text));
      await page.screenshot({ path: path.join(output, `images-blocked-${colorScheme}.png`), fullPage: true });
      await page.emulateMedia({ forcedColors: 'active' });
      assert.equal(await page.getByRole('link', { name: 'Reset password', exact: true }).isVisible(), true);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
      await page.screenshot({ path: path.join(output, `forced-colours-${colorScheme}.png`), fullPage: true });
      await context.close();
    }
    assert.equal(external.length, 0, 'no external requests');
    await fs.writeFile(path.join(root, 'render-report.json'), JSON.stringify(report, null, 2) + '\n');
    console.log(`Passed offline rendering at 1000/390/320px in light/dark preferences, blocked images and forced colours${reference ? ', matching approved Option B' : ''}.`);
  } finally {
    await browser.close();
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
