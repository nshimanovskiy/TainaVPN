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
    version: async () => 'preview',
    http: async (method, url, body) => {
      const r = await fetch(url, { method, body: body || undefined, headers: { 'Content-Type': 'application/json' } });
      return { status: r.status, body: await r.text() };
    },
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
  let res;
  try {
    res = await Native.http(method, url, body ? JSON.stringify(body) : '');
  } catch (e) {
    throw new Error('Нет связи с сервером: ' + (e.message || e));
  }
  if (res.error) throw new Error('Нет связи с сервером: ' + res.error);
  let data = {};
  try { data = JSON.parse(res.body || '{}'); } catch { throw new Error('Неверный ответ сервера (HTTP ' + res.status + ')'); }
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
  if (!list.length) throw new Error('В подписке нет прокси');
  return list.map((p, i) => ({
    name: p.name || data.name || 'Сервер',
    type: p.type === 'http' ? 'http' : 'socks',
    server: p.server,
    port: +p.port,
    username: p.username || '',
    password: p.password || '',
    sub: url,
    subIndex: i,
  }));
}

async function addSubscription(url) {
  url = url.trim();
  if (!/^https?:\/\//i.test(url)) throw new Error('Ссылка должна начинаться с https://');
  const items = await fetchSubscription(url);
  let firstId = null;
  for (const it of items) {
    const existing = store.profiles.find((p) => p.sub === url && p.subIndex === it.subIndex);
    if (existing) Object.assign(existing, it);
    else store.profiles.push({ id: uid(), ...it });
    firstId = firstId || (existing ? existing.id : store.profiles[store.profiles.length - 1].id);
  }
  if (!store.selected || !store.profiles.find((p) => p.id === store.selected)) store.selected = firstId;
  await save();
  render();
}

async function getFromServer() {
  const base = (store.serverUrl || DEFAULT_SERVER).replace(/\/+$/, '');
  const device = await Native.deviceName().catch(() => '');
  const data = await httpJSON('POST', base + '/api/v1/register', { device: device + ' (' + platform + ')' });
  await addSubscription(data.subscription_url);
  const added = store.profiles.find((p) => p.sub === data.subscription_url);
  if (added) store.selected = added.id;
  await save();
  render();
}

async function refreshProfile(p) {
  if (!p.sub) return;
  const items = await fetchSubscription(p.sub);
  const it = items.find((x) => x.subIndex === p.subIndex) || items[0];
  Object.assign(p, it);
  await save();
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
    inbounds = [tun];
  } else if (store.mode === 'proxy') {
    inbounds = [{ type: 'mixed', tag: 'mixed-in', listen: '127.0.0.1', listen_port: 2080, set_system_proxy: true }];
  } else {
    inbounds = [{ ...tun, strict_route: true }];
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
  if (!p) { toast('Сначала добавьте прокси'); openSheet('addSheet'); return; }
  busy = true;
  status = { state: 'starting', error: '' };
  renderStatus();
  try {
    if (p.sub) {
      try { await refreshProfile(p); } catch (e) { console.warn('refresh failed', e); }
    }
    await Native.start(buildConfig(p));
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
  busy = false;
  await pollStatus();
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

// ---------- rendering ----------

function renderStatus() {
  const pw = $('power');
  pw.className = 'power';
  const p = store.profiles.find((x) => x.id === store.selected);
  let text = 'Отключено';
  let sub = p ? (p.sub ? p.name : `${p.name} · ${p.server}:${p.port}`) : 'Добавьте прокси';
  switch (status.state) {
    case 'running': pw.classList.add('on'); text = 'Подключено'; break;
    case 'starting': pw.classList.add('busy'); text = 'Подключение…'; break;
    case 'stopping': pw.classList.add('busy'); text = 'Отключение…'; break;
    default:
      if (status.error) { pw.classList.add('err'); text = 'Ошибка'; sub = status.error; }
  }
  $('statusText').textContent = text;
  $('statusSub').textContent = sub;
}

function render() {
  const ul = $('profiles');
  ul.innerHTML = store.profiles.map((p) => `
    <li class="profile ${p.id === store.selected ? 'sel' : ''}" data-id="${p.id}">
      <span class="radio"></span>
      <div class="p-main">
        <div class="p-name">${esc(p.name || p.server)}${p.sub ? '<span class="tag">сервер</span>' : ''}</div>
        <div class="p-sub">${p.sub ? 'с сервера' : (p.type === 'http' ? 'HTTP' : 'SOCKS5') + ' · ' + esc(p.server) + ':' + esc(p.port) + (p.username ? ' · ' + esc(p.username) : '')}</div>
      </div>
      <div class="p-act">
        ${p.sub ? '<button data-act="refresh" title="Обновить">↻</button>' : ''}
        <button data-act="delete" title="Удалить">✕</button>
      </div>
    </li>`).join('');
  $('empty').hidden = store.profiles.length > 0;
  renderStatus();
}

function renderSettings() {
  $('serverUrl').value = store.serverUrl || DEFAULT_SERVER;
  $('bypassLan').checked = !!store.bypassLan;
  $('modeBox').hidden = platform === 'android';
  document.querySelectorAll('.seg button').forEach((b) => b.classList.toggle('active', b.dataset.mode === store.mode));
  $('modeHint').textContent = store.mode === 'tun'
    ? (platform === 'windows' ? 'Весь трафик устройства. Нужны права администратора.' : 'Весь трафик устройства. Нужны права root / CAP_NET_ADMIN.')
    : 'Только программы, которые используют системный прокси (браузеры и т.п.). Права не нужны.';
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
  btn.textContent = 'Подождите…';
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

  const doGet = async (btn) => {
    if (await withButton(btn, getFromServer)) { closeSheets(); toast('Прокси получен с сервера'); }
  };
  $('getFromServer').onclick = () => doGet($('getFromServer'));
  $('quickGet').onclick = () => doGet($('quickGet'));

  $('addSub').onclick = async () => {
    if (await withButton($('addSub'), () => addSubscription($('subUrl').value))) {
      $('subUrl').value = '';
      closeSheets();
      toast('Добавлено');
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
    if (!validProxy(p)) { $('addErr').textContent = 'Укажите корректные хост и порт'; return; }
    if (!p.name) p.name = p.server;
    store.profiles.push(p);
    if (!store.selected) store.selected = p.id;
    await save();
    ['mLink', 'mName', 'mHost', 'mPort', 'mUser', 'mPass'].forEach((id) => { $(id).value = ''; });
    closeSheets();
    render();
    toast('Прокси сохранён');
  };

  $('profiles').addEventListener('click', async (e) => {
    const li = e.target.closest('.profile');
    if (!li) return;
    const p = store.profiles.find((x) => x.id === li.dataset.id);
    const act = e.target.closest('button')?.dataset.act;
    if (act === 'delete') {
      if (!confirm(`Удалить «${p.name}»?`)) return;
      store.profiles = store.profiles.filter((x) => x !== p);
      if (store.selected === p.id) store.selected = store.profiles[0]?.id || null;
      await save(); render();
      return;
    }
    if (act === 'refresh') {
      try { await refreshProfile(p); render(); toast('Обновлено'); } catch (err) { toast(err.message); }
      return;
    }
    if (store.selected === p.id) return;
    store.selected = p.id;
    await save(); render();
    if (status.state === 'running') { toast('Переподключение…'); await connect(); }
  });

  $('serverUrl').addEventListener('change', async () => {
    store.serverUrl = $('serverUrl').value.trim().replace(/\/+$/, '') || DEFAULT_SERVER;
    await save();
  });
  $('bypassLan').addEventListener('change', async () => { store.bypassLan = $('bypassLan').checked; await save(); });
  document.querySelectorAll('.seg button').forEach((b) => {
    b.onclick = async () => {
      store.mode = b.dataset.mode; await save(); renderSettings();
      if (status.state === 'running') toast('Переподключитесь, чтобы применить режим');
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
  Native.version().then((v) => { $('version').textContent = v; }).catch(() => {});
  bind();
  render();
  await pollStatus();
  setInterval(pollStatus, 1000);
}

// exported for tests (node)
if (typeof module !== 'undefined') module.exports = { parseProxyLink, buildConfig, _setStore: (s, p) => { store = s; platform = p; } };
if (typeof document !== 'undefined') init();
