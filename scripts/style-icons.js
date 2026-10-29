// Renders the styles' icons from app/assets/styles/*.svg: the in-app logo, the
// Android launcher icons (adaptive foreground and monochrome, and the legacy
// square) and, for every style with the standard one, the white status bar
// icon of the notification and the tile. Run from the repository root with Playwright installed:
//   node scripts/style-icons.js [style...]
// Names of styles render only those, so the others' files stay as they are.
const { chromium } = require('playwright');
const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const only = process.argv.slice(2);
const pick = (name) => only.length === 0 || only.includes(name);
const styles = ['zarya', 'curse', 'miku'].filter(pick);
const res = 'app/android/app/src/main/res';
const densities = { mdpi: 1, hdpi: 1.5, xhdpi: 2, xxhdpi: 3, xxxhdpi: 4 };

(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  // The SVG drawn size×size in the middle of a canvas×canvas image on bg.
  async function render(svg, file, canvas, size, bg) {
    const body = svg.replace('<svg ', `<svg width="${size}" height="${size}" `);
    await page.setViewportSize({ width: canvas, height: canvas });
    await page.setContent(
      `<body style="margin:0;width:${canvas}px;height:${canvas}px;display:flex;align-items:center;` +
        `justify-content:center;background:${bg || 'transparent'}">${body}</body>`,
    );
    await page.screenshot({ path: file, omitBackground: !bg });
  }
  for (const name of styles) {
    const svg = fs.readFileSync(`app/assets/styles/${name}.svg`, 'utf8');
    await render(svg, `app/assets/styles/${name}.png`, 384, 384);
    for (const [d, k] of Object.entries(densities)) {
      const dir = path.join(res, `mipmap-${d}`);
      const fg = path.join(dir, `ic_launcher_${name}_foreground.png`);
      await render(svg, fg, 108 * k, Math.round(108 * k * 0.65));
      // Android tints the monochrome layer itself: only its shape counts.
      const mono = path.join(dir, `ic_launcher_${name}_monochrome.png`);
      execFileSync('convert', [fg, '-fill', 'white', '-colorize', '100', mono]);
      await render(svg, path.join(dir, `ic_launcher_${name}.png`), 48 * k, Math.round(48 * k * 0.86), '#09090B');
    }
    fs.writeFileSync(
      path.join(res, 'mipmap-anydpi-v26', `ic_launcher_${name}.xml`),
      `<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/ic_launcher_background" />
    <foreground android:drawable="@mipmap/ic_launcher_${name}_foreground" />
    <monochrome android:drawable="@mipmap/ic_launcher_${name}_monochrome" />
</adaptive-icon>
`,
    );
  }
  // Status bar icons: the shape alone, without the glow, lines a bit bolder.
  const all = { tiger: 'app/assets/tiger.svg', ...Object.fromEntries(styles.map((n) => [n, `app/assets/styles/${n}.svg`])) };
  for (const [name, file] of Object.entries(all).filter(([name]) => pick(name))) {
    const svg = fs
      .readFileSync(file, 'utf8')
      .replace(/ filter="url\(#glow\)"/g, '')
      .replace(/stroke-width="([\d.]+)"/g, (_, w) => `stroke-width="${Math.max(w * 1.6, 1)}"`);
    for (const [d, k] of Object.entries(densities)) {
      const dir = path.join(res, `drawable-${d}`);
      fs.mkdirSync(dir, { recursive: true });
      const out = path.join(dir, `ic_stat_${name}.png`);
      await render(svg, out, 24 * k, Math.round(24 * k * 1.04));
      execFileSync('convert', [out, '-fill', 'white', '-colorize', '100', out]);
    }
  }
  await browser.close();
})();
