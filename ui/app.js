'use strict';

// Address of your Tainavpn server (can be changed in the app settings).
const DEFAULT_SERVER = 'https://vpn.sdsds.top';

// ---------- native bridge (Android WebView / Wails desktop / browser mock) ----------

function androidBridge(A) {
  return {
    kind: 'android',
    platform: async () => 'android',
    loadStore: async () => A.loadStore(),
    saveStore: async (s) => { A.saveStore(s); },
    start: async (cfg) => { const e = A.start(cfg); if (e) throw new Error(e); },
    stop: async () => { A.stop(); },
    status: async () => JSON.parse(A.status()),
    logs: async () => A.logs(),
    deviceName: async () => A.deviceName(),
    version: async () => A.version(),
    http: async (method, url, body) => JSON.parse(A.http(method, url, body || '')),
    setLang: async (l) => { if (A.setLang) A.setLang(l); },
    openUrl: async (u) => { A.openUrl(u); },
    appVersion: async () => (A.appVersion ? A.appVersion() : ''),
    ping: (p) => new Promise((resolve, reject) => {
      const id = 'p' + Math.random().toString(36).slice(2);
      pingWaiters[id] = { resolve, reject };
      A.pingAsync(id, JSON.stringify(p));
    }),
    update: async (url, sha) => { A.update(url, sha || ''); },
    setKillSwitch: async (on) => { A.setKillSwitch(!!on); },
    openVpnSettings: async () => { A.openVpnSettings(); },
    updateStatus: async () => JSON.parse(A.updateStatus()),
  };
}

function desktopBridge() {
  const app = () => window.go.main.App;
  return {
    kind: 'desktop',
    platform: () => app().Platform(),
    loadStore: () => app().LoadStore(),
    saveStore: (s) => app().SaveStore(s),
    start: (cfg) => app().Start(cfg),
    stop: () => app().Stop(),
    status: async () => JSON.parse(await app().Status()),
    logs: () => app().Logs(),
    deviceName: () => app().DeviceName(),
    version: () => app().Version(),
    http: async (method, url, body) => JSON.parse(await app().HTTP(method, url, body || '')),
    setLang: (l) => app().SetLang(l),
    openUrl: (u) => app().OpenURL(u),
    appVersion: () => app().AppVersion(),
    ping: (p) => app().Ping(JSON.stringify(p)),
    update: (url, sha) => app().Update(url, sha || ''),
    killSwitch: (on, hosts) => app().KillSwitch(!!on, JSON.stringify(hosts || [])),
    updateStatus: async () => JSON.parse(await app().UpdateStatus()),
  };
}

function mockBridge() {
  let state = { state: 'stopped', error: '' };
  return {
    kind: 'mock',
    platform: async () => 'browser',
    loadStore: async () => localStorage.getItem('tvpn') || '',
    saveStore: async (s) => localStorage.setItem('tvpn', s),
    start: async (cfg) => { console.log(cfg); state = { state: 'running', error: '' }; },
    stop: async () => { state = { state: 'stopped', error: '' }; },
    status: async () => state,
    logs: async () => '(browser preview)',
    deviceName: async () => 'browser',
    openUrl: async (u) => { window.open(u, '_blank'); },
    appVersion: async () => '0.0.0',
    ping: async () => { await new Promise((r) => setTimeout(r, 300 + Math.random() * 700)); return Math.round(80 + Math.random() * 900); },
    update: async () => {},
    killSwitch: async () => {},
    updateStatus: async () => ({ state: 'idle' }),
    version: async () => 'preview',
    http: async (method, url, body) => {
      const r = await fetch(url, { method, body: body || undefined, headers: { 'Content-Type': 'application/json' } });
      return { status: r.status, body: await r.text() };
    },
  };
}

// Android answers ping requests asynchronously through this callback
const pingWaiters = {};
if (typeof window !== 'undefined') {
  window.__tvpnPingDone = (id, ms, err) => {
    const w = pingWaiters[id];
    delete pingWaiters[id];
    if (w) { if (err) w.reject(new Error(err)); else w.resolve(ms); }
  };
}

async function getNative() {
  if (window.TainaAndroid) return androidBridge(window.TainaAndroid);
  for (let i = 0; i < 50; i++) {
    if (window.go && window.go.main && window.go.main.App) return desktopBridge();
    await new Promise((r) => setTimeout(r, 100));
  }
  return mockBridge();
}

// ---------- state ----------

let Native;
let platform = 'browser';
let store = { profiles: [], selected: null, mode: 'tun', serverUrl: DEFAULT_SERVER, bypassLan: true };
let status = { state: 'stopped', error: '' };
let busy = false;
let botUrl = '';

const $ = (id) => document.getElementById(id);
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const uid = () => Math.random().toString(36).slice(2, 10);

async function save() { await Native.saveStore(JSON.stringify(store)); }

function toast(msg, ms = 2600) {
  const t = $('toast');
  t.textContent = msg;
  t.hidden = false;
  clearTimeout(toast._t);
  toast._t = setTimeout(() => { t.hidden = true; }, ms);
}

// ---------- HTTP helpers (performed natively: no CORS, works when VPN is off) ----------

async function httpJSON(method, url, body) {
  // tell our server which language to answer errors in
  if (/\/(api\/v1\/|sub\/)/.test(url)) url += (url.includes('?') ? '&' : '?') + 'lang=' + LANG;
  let res;
  try {
    res = await Native.http(method, url, body ? JSON.stringify(body) : '');
  } catch (e) {
    throw new Error(t('errNoConnection', e.message || e));
  }
  if (res.error) throw new Error(t('errNoConnection', res.error));
  let data = {};
  try { data = JSON.parse(res.body || '{}'); } catch { throw new Error(t('errBadResponse', res.status)); }
  if (res.status < 200 || res.status >= 300) throw new Error(data.error || ('HTTP ' + res.status));
  return data;
}

// ---------- profiles ----------

function parseProxyLink(text) {
  text = text.trim();
  if (!text) return null;
  // socks5://user:pass@host:port#name, http://user:pass@host:port, user:pass@host:port
  const m = text.match(/^(?:(socks5?h?|http):\/\/)?(?:([^:@/]*)(?::([^@/]*))?@)?(\[[^\]]+\]|[^:/#?@]+):(\d+)\/?(?:#(.*))?$/i);
  if (m && (m[1] || m[2] !== undefined)) {
    const dec = (x) => { try { return decodeURIComponent(x || ''); } catch { return x || ''; } };
    return {
      type: m[1] && m[1].toLowerCase() === 'http' ? 'http' : 'socks',
      username: dec(m[2]),
      password: dec(m[3]),
      server: m[4].replace(/^\[|\]$/g, ''),
      port: +m[5],
      name: m[6] ? dec(m[6]) : '',
    };
  }
  // host:port:user:pass  or host:port
  const parts = text.split(':');
  if (parts.length === 2 || parts.length === 4) {
    const port = +parts[1];
    if (parts[0] && port > 0 && port < 65536) {
      return { type: 'socks', server: parts[0], port, username: parts[2] || '', password: parts[3] || '', name: '' };
    }
  }
  return null;
}

function validProxy(p) {
  return p && p.server && /^[\w.\-:]+$/.test(p.server) && p.port > 0 && p.port < 65536;
}

async function fetchSubscription(url) {
  const data = await httpJSON('GET', url);
  const list = (data.proxies || []).filter((p) => !p.type || p.type === 'socks' || p.type === 'http');
  if (!list.length) throw new Error(t('errEmptySub'));
  const seen = {};
  return list.map((p, i) => {
    const country = (p.country || '').toUpperCase();
    // stable key inside the subscription: the country (or position if unknown)
    let subKey = country || 'n' + i;
    if (seen[subKey]) subKey += '-' + i;
    seen[subKey] = true;
    return {
      name: p.country_name || '',
      country,
      type: p.type === 'http' ? 'http' : 'socks',
      server: p.server,
      port: +p.port,
      username: p.username || '',
      password: p.password || '',
      sub: url,
      subKey,
    };
  });
}

// syncSubscription replaces all profiles of a subscription with the fresh list,
// keeping profile ids (and the selection) for countries that are still there.
async function syncSubscription(url) {
  const items = await fetchSubscription(url);
  const old = store.profiles.filter((p) => p.sub === url);
  const fresh = items.map((it) => {
    const prev = old.find((p) => p.subKey === it.subKey);
    return { ...(prev || {}), ...it, id: prev ? prev.id : uid() };
  });
  const firstIdx = store.profiles.findIndex((p) => p.sub === url);
  const others = store.profiles.filter((p) => p.sub !== url);
  if (firstIdx < 0) others.push(...fresh);
  else others.splice(Math.min(firstIdx, others.length), 0, ...fresh);
  store.profiles = others;
  if (!store.profiles.find((p) => p.id === store.selected)) {
    store.selected = (fresh[0] || store.profiles[0] || {}).id || null;
  }
  await save();
  return fresh;
}

async function addSubscription(url) {
  url = url.trim();
  if (!/^https?:\/\//i.test(url)) throw new Error(t('errLinkHttps'));
  const fresh = await syncSubscription(url);
  if (fresh[0] && !store.profiles.find((p) => p.id === store.selected && p.sub)) store.selected = fresh[0].id;
  await save();
  render();
}

// fetchInfo asks our server for the Telegram bot link (shown when the list is empty)
async function fetchInfo() {
  const base = (store.serverUrl || DEFAULT_SERVER).replace(/\/+$/, '');
  try {
    const info = await httpJSON('GET', base + '/api/v1/info');
    botUrl = /^https:\/\/t\.me\/\w+$/.test(info.bot_url || '') ? info.bot_url : '';
  } catch (e) {
    botUrl = '';
  }
  $('openBot').hidden = !botUrl;
  $('openBot2').hidden = !botUrl;
}

// refreshProfile updates the whole subscription the profile belongs to
// and returns the (possibly new) profile for the same country.
async function refreshProfile(p) {
  if (!p.sub) return p;
  const fresh = await syncSubscription(p.sub);
  return fresh.find((x) => x.subKey === p.subKey) || fresh[0] || p;
}

// detectCountry asks our server which country a manually added proxy exits in.
async function detectCountry(p) {
  const base = (store.serverUrl || DEFAULT_SERVER).replace(/\/+$/, '');
  try {
    const r = await httpJSON('POST', base + '/api/v1/geo', { type: p.type, server: p.server, port: +p.port, username: p.username, password: p.password });
    p.country = (r.country || '').toUpperCase();
    p.countryName = r.country_name || '';
  } catch (e) {
    console.warn('country detection failed', e);
  }
}

// ---------- flags & names ----------

const regionNamesCache = {};
function regionName(cc) {
  try {
    if (!regionNamesCache[LANG]) regionNamesCache[LANG] = new Intl.DisplayNames([LANG], { type: 'region' });
    const n = regionNamesCache[LANG].of(cc);
    return n && n !== cc ? n : '';
  } catch { return ''; }
}

// countryName is shown in the UI language; the server-provided (Russian) name is a fallback
function countryName(p) {
  const cc = (p.country || '').toUpperCase();
  if (cc) {
    if (LANG === 'en' && cc === 'US') return 'USA';
    if (LANG === 'ru' && cc === 'US') return 'США';
    const n = regionName(cc);
    if (n) return n;
  }
  if (p.sub && p.name) return p.name;
  return p.countryName || '';
}

function flagSrc(cc) {
  cc = (cc || '').toLowerCase();
  return /^[a-z]{2}$/.test(cc) ? `flags/${cc}.svg` : 'flags/xx.svg';
}

// profileTitle: what the user sees — the country, or a fallback name
function profileTitle(p) {
  const name = countryName(p);
  if (p.sub) return name || t('serverFallback');
  return name || p.label || p.name || p.server;
}

// ---------- sing-box config ----------

function buildConfig(p) {
  const proxy = p.type === 'http'
    ? { type: 'http', tag: 'proxy', server: p.server, server_port: +p.port }
    : { type: 'socks', tag: 'proxy', server: p.server, server_port: +p.port, version: '5' };
  if (p.username) { proxy.username = p.username; proxy.password = p.password || ''; }

  const rules = [
    { action: 'sniff' },
    { protocol: 'dns', action: 'hijack-dns' },
  ];
  if (platform === 'android') {
    // Android "Private DNS" (DNS-over-TLS, port 853): purchased proxies usually block 853,
    // so send it directly (it is encrypted anyway); probes to the VPN's own DNS address fail fast.
    rules.push({ ip_cidr: ['172.19.0.2/32'], port: 853, action: 'reject' });
    rules.push({ network: 'tcp', port: 853, outbound: 'direct' });
  }
  // traffic to the proxy servers themselves (e.g. the ping check) goes direct, not proxy-in-proxy
  const servers = [...new Set(store.profiles.map((x) => String(x.server || '')).filter(Boolean))];
  const ipServers = servers.filter((h) => /^[\d.]+$|:/.test(h)).map((h) => (h.includes(':') ? h + '/128' : h + '/32'));
  const nameServers = servers.filter((h) => !/^[\d.]+$|:/.test(h));
  if (ipServers.length) rules.push({ ip_cidr: ipServers, outbound: 'direct' });
  if (nameServers.length) rules.push({ domain: nameServers, outbound: 'direct' });
  if (store.bypassLan) rules.push({ ip_is_private: true, outbound: 'direct' });
  // QUIC through SOCKS5 is unreliable: block it so browsers fall back to TCP
  rules.push({ network: 'udp', port: 443, action: 'reject' });

  const tun = {
    type: 'tun',
    tag: 'tun-in',
    address: ['172.19.0.1/30', 'fdfe:dcba:9876::1/126'],
    mtu: 9000,
    auto_route: true,
    stack: 'mixed',
  };
  let inbounds;
  if (platform === 'android') {
    // gvisor: with the "system"/"mixed" TCP stack Android's own DNS-over-TLS client (Private DNS)
    // can't connect through the tunnel and all name resolution breaks (reproduced on an emulator)
    inbounds = [{ ...tun, stack: 'gvisor' }];
  } else if (store.mode === 'proxy') {
    inbounds = [{ type: 'mixed', tag: 'mixed-in', listen: '127.0.0.1', listen_port: 2080, set_system_proxy: true }];
  } else {
    inbounds = [{ ...tun, interface_name: 'tainavpn', strict_route: true }];
  }

  return JSON.stringify({
    log: { level: 'info', timestamp: true },
    dns: {
      servers: [
        { type: 'tcp', tag: 'remote', server: '1.1.1.1', detour: 'proxy' },
        { type: 'udp', tag: 'local', server: '77.88.8.8' },
      ],
      final: 'remote',
      strategy: 'prefer_ipv4',
      reverse_mapping: true,
    },
    inbounds,
    outbounds: [proxy, { type: 'direct', tag: 'direct' }],
    route: {
      rules,
      final: 'proxy',
      auto_detect_interface: true,
      default_domain_resolver: 'local',
    },
  }, null, 2);
}

// ---------- connect / disconnect ----------

async function connect() {
  const p = store.profiles.find((x) => x.id === store.selected);
  if (!p) { toast(t('addProxyFirst')); openSheet('addSheet'); return; }
  busy = true;
  status = { state: 'starting', error: '' };
  renderStatus();
  try {
    let cur = p;
    if (p.sub) {
      try { cur = await refreshProfile(p); store.selected = cur.id; await save(); render(); } catch (e) { console.warn('refresh failed', e); }
    }
    if (desktopKillSwitch()) {
      // before starting: from now on only the VPN and the proxy servers are reachable
      await Native.killSwitch(true, proxyHosts());
    }
    await Native.start(buildConfig(cur));
  } catch (e) {
    status = { state: 'stopped', error: String(e.message || e) };
  } finally {
    busy = false;
  }
  await pollStatus();
}

async function disconnect() {
  busy = true;
  try { await Native.stop(); } catch (e) { toast(String(e.message || e)); }
  // switching off on purpose: lift the kill switch block
  if (Native.killSwitch && platform !== 'android') { try { await Native.killSwitch(false, []); } catch (e) { toast(String(e.message || e)); } }
  busy = false;
  await pollStatus();
}

// ---------- kill switch ----------

function desktopKillSwitch() {
  return platform !== 'android' && store.killSwitch && store.mode !== 'proxy' && !!Native.killSwitch;
}

function proxyHosts() {
  return [...new Set(store.profiles.map((p) => String(p.server || '')).filter(Boolean))];
}

async function unblock() {
  busy = true;
  try {
    if (platform === 'android') await Native.stop();
    else await Native.killSwitch(false, []);
  } catch (e) { toast(String(e.message || e)); }
  busy = false;
  status = { state: 'stopped', error: '' };
  await pollStatus();
}

async function setKillSwitch(on) {
  store.killSwitch = on;
  await save();
  if (platform === 'android') { await Native.setKillSwitch(on).catch(() => {}); return; }
  try {
    if (!on) await Native.killSwitch(false, []);
    else if (status.state === 'running' && store.mode !== 'proxy') await Native.killSwitch(true, proxyHosts());
  } catch (e) { toast(String(e.message || e)); }
}

async function pollStatus() {
  if (busy) return;
  try {
    const s = await Native.status();
    // keep an error produced by start() until the user tries again
    if (!(s.state === 'stopped' && !s.error && status.error && status.state === 'stopped')) status = s;
  } catch (e) { /* ignore */ }
  renderStatus();
}

// ---------- ping ----------

const pingRes = {}; // profile id -> { ms } | { err } | { pending: true }

function pingBadge(p) {
  const r = pingRes[p.id];
  if (!r) return '';
  if (r.pending) return '<span class="ping wait">…</span>';
  if (r.err) return `<span class="ping bad" title="${esc(r.err)}">✕</span>`;
  const cls = r.ms < 300 ? 'good' : r.ms < 800 ? 'mid' : 'bad';
  return `<span class="ping ${cls}">${r.ms} ${esc(t('ms'))}</span>`;
}

let pinging = false;
async function pingAll() {
  if (pinging || !store.profiles.length) return;
  pinging = true;
  $('pingAll').disabled = true;
  const list = [...store.profiles];
  list.forEach((p) => { pingRes[p.id] = { pending: true }; });
  render();
  let i = 0;
  const worker = async () => {
    while (i < list.length) {
      const p = list[i++];
      try {
        const ms = await Native.ping({ type: p.type, server: p.server, port: +p.port, username: p.username || '', password: p.password || '' });
        pingRes[p.id] = { ms };
      } catch (e) {
        pingRes[p.id] = { err: String(e.message || e) };
      }
      render();
    }
  };
  await Promise.all([worker(), worker(), worker(), worker()]);
  pinging = false;
  $('pingAll').disabled = false;
}

// ---------- updates ----------

const RELEASES_API = 'https://api.github.com/repos/nshimanovskiy/TainaVPN/releases/latest';
let updateInfo = null; // { version, url, sha }

function newerThan(a, b) {
  const pa = String(a).replace(/^v/, '').split('.').map(Number);
  const pb = String(b).replace(/^v/, '').split('.').map(Number);
  for (let i = 0; i < 3; i++) {
    if ((pa[i] || 0) !== (pb[i] || 0)) return (pa[i] || 0) > (pb[i] || 0);
  }
  return false;
}

function assetFor(assets) {
  const re = platform === 'windows' ? /windows.*\.exe$/i : platform === 'android' ? /android\.apk$/i : platform === 'linux' ? /_amd64\.deb$/i : null;
  return re ? (assets || []).find((a) => re.test(a.name)) : null;
}

async function checkUpdate(manual) {
  if (!['windows', 'android', 'linux'].includes(platform)) return;
  try {
    const current = await Native.appVersion();
    const rel = await httpJSON('GET', RELEASES_API);
    const asset = assetFor(rel.assets);
    if (asset && /^\d+\.\d+\.\d+$/.test(current) && newerThan(rel.tag_name, current)) {
      updateInfo = { version: String(rel.tag_name).replace(/^v/, ''), url: asset.browser_download_url, sha: asset.digest || '' };
      $('updateText').textContent = t('updateAvailable', updateInfo.version);
      $('updateBar').hidden = false;
    } else {
      updateInfo = null;
      $('updateBar').hidden = true;
      if (manual) toast(t('upToDate'));
    }
  } catch (e) {
    if (manual) toast(t('updateCheckFailed', e.message || e));
  }
}

async function runUpdate() {
  if (!updateInfo) return;
  const btn = $('updateBtn');
  btn.disabled = true;
  try {
    await Native.update(updateInfo.url, updateInfo.sha);
  } catch (e) {
    toast(String(e.message || e)); btn.disabled = false; return;
  }
  const timer = setInterval(async () => {
    let st;
    try { st = await Native.updateStatus(); } catch { return; }
    if (st.state === 'downloading') {
      $('updateText').textContent = t('updateDownloading', Math.round((st.progress || 0) * 100));
    } else if (st.state === 'installing') {
      $('updateText').textContent = t('updateInstalling');
    } else if (st.state === 'error') {
      clearInterval(timer);
      $('updateText').textContent = t('updateFailed', st.error);
      btn.disabled = false;
    } else if (st.state === 'idle') {
      clearInterval(timer);
      $('updateText').textContent = t('updateAvailable', updateInfo.version);
      btn.disabled = false;
    }
  }, 500);
}

// ---------- rendering ----------

function renderStatus() {
  const pw = $('power');
  pw.className = 'power';
  const p = store.profiles.find((x) => x.id === store.selected);
  let text = t('disconnected');
  let sub = p ? profileTitle(p) : t('addProxyFirst');
  switch (status.state) {
    case 'running': pw.classList.add('on'); text = t('connected'); break;
    case 'starting': pw.classList.add('busy'); text = t('connecting'); break;
    case 'stopping': pw.classList.add('busy'); text = t('disconnecting'); break;
    case 'blocked': pw.classList.add('err'); text = t('blocked'); sub = t('blockedHint'); break;
    default:
      if (status.error) { pw.classList.add('err'); text = t('error'); sub = status.error; }
  }
  $('statusText').textContent = text;
  $('statusFlag').hidden = !p || (status.error && status.state === 'stopped');
  if (p) $('statusFlag').src = flagSrc(p.country);
  $('statusSub').textContent = sub;
  $('unblockBtn').hidden = status.state !== 'blocked';
}

function render() {
  const ul = $('profiles');
  ul.innerHTML = store.profiles.map((p) => `
    <li class="profile ${p.id === store.selected ? 'sel' : ''}" data-id="${p.id}">
      <span class="radio"></span>
      <img class="flag" src="${flagSrc(p.country)}" alt="">
      <div class="p-main">
        <div class="p-name">${esc(profileTitle(p))}</div>
        <div class="p-sub">${p.sub ? 'Tainavpn' : t('ownProxy') + (p.label && countryName(p) ? ' · ' + esc(p.label) : '') + ' · ' + esc(p.server) + ':' + esc(p.port)}</div>
      </div>
      <div class="p-act">
        ${pingBadge(p)}
        ${p.sub ? `<button data-act="refresh" title="${esc(t('refreshList'))}">↻</button>` : ''}
        <button data-act="delete" title="${esc(t('delete'))}">✕</button>
      </div>
    </li>`).join('');
  $('empty').hidden = store.profiles.length > 0;
  renderStatus();
}

function applyLang() {
  setLang(store.lang || 'auto');
  if (Native && Native.setLang) Native.setLang(LANG).catch(() => {});
}

function renderSettings() {
  $('langSel').value = store.lang || 'auto';
  $('killSwitch').checked = !!store.killSwitch;
  $('vpnSettings').hidden = platform !== 'android';
  $('ksHint').textContent = platform === 'android' ? t('ksHintAndroid')
    : store.mode === 'proxy' ? t('ksHintProxyMode') : t('ksHintDesktop');
  $('serverUrl').value = store.serverUrl || DEFAULT_SERVER;
  $('bypassLan').checked = !!store.bypassLan;
  $('modeBox').hidden = platform === 'android';
  document.querySelectorAll('.seg button').forEach((b) => b.classList.toggle('active', b.dataset.mode === store.mode));
  $('modeHint').textContent = store.mode === 'tun'
    ? (platform === 'windows' ? t('modeHintTunWin') : t('modeHintTunLinux'))
    : t('modeHintProxy');
}

function openSheet(id) {
  $(id).hidden = false;
  if (id === 'settingsSheet') {
    renderSettings();
    Native.logs().then((l) => { $('logs').textContent = l || '—'; });
  }
  $('addErr').textContent = '';
}
function closeSheets() { document.querySelectorAll('.sheet').forEach((s) => { s.hidden = true; }); }

async function withButton(btn, fn) {
  const old = btn.textContent;
  btn.disabled = true;
  btn.textContent = t('pleaseWait');
  $('addErr').textContent = '';
  try { await fn(); return true; } catch (e) { $('addErr').textContent = String(e.message || e); toast(String(e.message || e)); return false; } finally { btn.disabled = false; btn.textContent = old; }
}

// ---------- events ----------

function bind() {
  $('power').onclick = () => {
    if (busy || status.state === 'starting' || status.state === 'stopping') return;
    if (status.state === 'running') disconnect(); else connect();
  };
  $('openAdd').onclick = () => openSheet('addSheet');
  $('openSettings').onclick = () => openSheet('settingsSheet');
  document.querySelectorAll('[data-close]').forEach((b) => { b.onclick = closeSheets; });
  document.querySelectorAll('.sheet').forEach((s) => s.addEventListener('click', (e) => { if (e.target === s) closeSheets(); }));

  document.querySelectorAll('.tab').forEach((t) => {
    t.onclick = () => {
      document.querySelectorAll('.tab').forEach((x) => x.classList.toggle('active', x === t));
      document.querySelectorAll('.tab-body').forEach((b) => { b.hidden = b.dataset.body !== t.dataset.tab; });
      $('addErr').textContent = '';
    };
  });

  const openBot = () => { if (botUrl) Native.openUrl(botUrl).catch((e) => toast(String(e.message || e))); };
  $('openBot').onclick = openBot;
  $('pingAll').onclick = pingAll;
  $('updateBtn').onclick = runUpdate;
  $('checkUpdate').onclick = () => checkUpdate(true);
  $('openBot2').onclick = openBot;
  $('emptyAdd').onclick = () => openSheet('addSheet');

  $('addSub').onclick = async () => {
    if (await withButton($('addSub'), () => addSubscription($('subUrl').value))) {
      $('subUrl').value = '';
      closeSheets();
      toast(t('added'));
    }
  };

  $('mLink').addEventListener('input', () => {
    const p = parseProxyLink($('mLink').value);
    if (!p) return;
    $('mHost').value = p.server; $('mPort').value = p.port;
    $('mUser').value = p.username; $('mPass').value = p.password;
    $('mType').value = p.type;
    if (p.name) $('mName').value = p.name;
  });

  $('addManual').onclick = async () => {
    const p = {
      id: uid(),
      type: $('mType').value,
      name: $('mName').value.trim(),
      server: $('mHost').value.trim(),
      port: +$('mPort').value.trim(),
      username: $('mUser').value.trim(),
      password: $('mPass').value,
    };
    if (!validProxy(p)) { $('addErr').textContent = t('errHostPort'); return; }
    p.label = p.name; // user's own title, if any
    const btn = $('addManual'); btn.disabled = true; btn.textContent = t('detectingCountry');
    await detectCountry(p);
    btn.disabled = false; btn.textContent = t('save');
    if (!p.country && !p.label) p.label = p.server;
    store.profiles.push(p);
    if (!store.selected) store.selected = p.id;
    await save();
    ['mLink', 'mName', 'mHost', 'mPort', 'mUser', 'mPass'].forEach((id) => { $(id).value = ''; });
    closeSheets();
    render();
    toast(t('proxySaved'));
  };

  $('profiles').addEventListener('click', async (e) => {
    const li = e.target.closest('.profile');
    if (!li) return;
    const p = store.profiles.find((x) => x.id === li.dataset.id);
    const act = e.target.closest('button')?.dataset.act;
    if (act === 'delete') {
      if (p.sub) {
        if (!confirm(t('confirmDeleteServer'))) return;
        store.profiles = store.profiles.filter((x) => x.sub !== p.sub);
      } else {
        if (!confirm(t('confirmDelete', profileTitle(p)))) return;
        store.profiles = store.profiles.filter((x) => x !== p);
      }
      if (store.selected === p.id) store.selected = store.profiles[0]?.id || null;
      await save(); render();
      return;
    }
    if (act === 'refresh') {
      try { await syncSubscription(p.sub); render(); toast(t('listUpdated')); } catch (err) { toast(err.message); }
      return;
    }
    if (store.selected === p.id) return;
    store.selected = p.id;
    await save(); render();
    if (status.state === 'running') { toast(t('reconnecting')); await connect(); }
  });

  $('serverUrl').addEventListener('change', async () => {
    store.serverUrl = $('serverUrl').value.trim().replace(/\/+$/, '') || DEFAULT_SERVER;
    await save();
    fetchInfo();
  });
  $('killSwitch').addEventListener('change', async () => { await setKillSwitch($('killSwitch').checked); renderSettings(); });
  $('vpnSettings').onclick = () => Native.openVpnSettings().catch(() => {});
  $('unblockBtn').onclick = unblock;
  $('langSel').addEventListener('change', async () => {
    store.lang = $('langSel').value;
    await save();
    applyLang();
  if (platform === 'android' && Native.setKillSwitch) Native.setKillSwitch(!!store.killSwitch).catch(() => {});
    render();
    renderSettings();
  });
  $('bypassLan').addEventListener('change', async () => { store.bypassLan = $('bypassLan').checked; await save(); });
  document.querySelectorAll('.seg button').forEach((b) => {
    b.onclick = async () => {
      store.mode = b.dataset.mode; await save(); renderSettings();
      if (status.state === 'running') toast(t('reconnectForMode'));
    };
  });
}

async function init() {
  Native = await getNative();
  platform = await Native.platform();
  try {
    const raw = await Native.loadStore();
    if (raw) store = { ...store, ...JSON.parse(raw) };
  } catch (e) { console.warn('bad store', e); }
  applyLang();
  if (platform === 'android' && Native.setKillSwitch) Native.setKillSwitch(!!store.killSwitch).catch(() => {});
  Native.version().then((v) => { $('version').textContent = v; }).catch(() => {});
  bind();
  render();
  fetchInfo();
  setTimeout(() => checkUpdate(false), 3000);
  await pollStatus();
  setInterval(pollStatus, 1000);
}

// exported for tests (node)
if (typeof module !== 'undefined') module.exports = { parseProxyLink, buildConfig, _setStore: (s, p) => { store = s; platform = p; } };
if (typeof document !== 'undefined') init();
