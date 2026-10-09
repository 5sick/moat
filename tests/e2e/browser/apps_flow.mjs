// 실행 중인 앱 → 공개 화면 시험 (edge_flow.sh가 Hub·Agent를 띄운 뒤 실행)
import { chromium } from 'playwright';

const { BASE, SESSION, APP_PORT, SHOT } = process.env;
let failed = 0;
const ok = (m) => console.log(`  ok   ${m}`);
const bad = (m) => { console.log(`  FAIL ${m}`); failed++; };

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext({ locale: 'ko-KR', viewport: { width: 1100, height: 800 } });
  const [name, value] = SESSION.split('=');
  await ctx.addCookies([{ name, value, domain: 'localhost', path: '/' }]);
  const page = await ctx.newPage();
  const errs = [];
  page.on('pageerror', (e) => errs.push(String(e)));
  page.on('console', (m) => { if (m.type() === 'error' && !m.text().includes('404')) errs.push(m.text()); });

  await page.goto(`${BASE}/services`);
  const row = page.locator('#apps li', { hasText: `포트 ${APP_PORT}` });
  await row.waitFor({ timeout: 10000 }).then(() => ok('실행 중인 앱 목록에 표시'), () => bad('앱 목록'));
  (await row.innerText()).includes('이 서버 안에서만') ? ok('127.0.0.1 전용 표시') : bad('루프백 표시');
  if (SHOT) await page.screenshot({ path: SHOT, fullPage: true });
  await row.getByRole('button', { name: '공개' }).click();
  await page.locator('#svc-dialog[open]').waitFor({ timeout: 5000 });
  const upstream = await page.inputValue('#svc-upstream');
  const host = await page.inputValue('#svc-host');
  upstream === `127.0.0.1:${APP_PORT}` ? ok('공개 → 업스트림 채움') : bad(`업스트림 ${upstream}`);
  /\.localhost$/.test(host) ? ok(`공개 → 도메인 채움 (${host})`) : bad(`도메인 ${host}`);
  await page.locator('#svc-dialog').evaluate((d) => d.close());
  await row.getByRole('button', { name: '숨기기' }).click();
  await page.locator('#apps-hidden li', { hasText: `포트 ${APP_PORT}` }).waitFor({ state: 'attached', timeout: 3000 })
    .then(() => ok('숨기기'), () => bad('숨기기'));
  errs.length === 0 ? ok('콘솔·CSP 오류 없음') : bad(`콘솔 오류: ${errs.join(' | ')}`);
} finally {
  await browser.close();
}
process.exit(failed ? 1 : 0);
