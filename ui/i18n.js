'use strict';

// Interface strings. Add a language by adding another block with the same keys.
const I18N = {
  ru: {
    connect: 'Подключить',
    disconnected: 'Отключено',
    connected: 'Подключено',
    connecting: 'Подключение…',
    disconnecting: 'Отключение…',
    error: 'Ошибка',
    proxies: 'Прокси',
    add: '+ Добавить',
    noProxiesYet: 'Прокси ещё нет.',
    getLinkInBot: 'Ссылку-подписку выдаёт наш Telegram-бот. Добавьте её через «+ Добавить».',
    openBot: 'Открыть Telegram-бота',
    addByLinkShort: 'Добавить по ссылке',
    tabByLink: 'По ссылке',
    pasteLinkHint: 'Вставьте ссылку-подписку из Telegram-бота',
    addProxy: 'Добавить прокси',
    tabManual: 'Свой прокси',
    subUrlPh: 'https://…/sub/ключ',
    addByLink: 'Добавить по ссылке',
    manualLinkPh: 'логин:пароль@хост:порт',
    orFillFields: 'или заполните поля',
    namePh: 'Название (необязательно)',
    hostPh: 'Хост / IP',
    portPh: 'Порт',
    userPh: 'Логин (если есть)',
    passPh: 'Пароль',
    save: 'Сохранить',
    settings: 'Настройки',
    serverAddress: 'Адрес сервера Tainavpn',
    mode: 'Режим',
    modeTun: 'Весь трафик (TUN)',
    modeProxy: 'Системный прокси',
    modeHintTunWin: 'Весь трафик устройства. Нужны права администратора.',
    modeHintTunLinux: 'Весь трафик устройства. Нужны права root / CAP_NET_ADMIN.',
    modeHintProxy: 'Только программы, которые используют системный прокси (браузеры и т.п.). Права не нужны.',
    bypassLan: 'Локальная сеть напрямую',
    language: 'Язык',
    langAuto: 'Как в системе',
    coreLogs: 'Логи ядра',
    close: 'Закрыть',
    refreshList: 'Обновить список с сервера',
    delete: 'Удалить',
    ownProxy: 'свой прокси',
    serverFallback: 'Сервер',
    pleaseWait: 'Подождите…',
    detectingCountry: 'Определяем страну…',
    errNoConnection: 'Нет связи с сервером: {0}',
    errBadResponse: 'Неверный ответ сервера (HTTP {0})',
    errEmptySub: 'В подписке нет прокси',
    errLinkHttps: 'Ссылка должна начинаться с https://',
    errHostPort: 'Укажите корректные хост и порт',
    addProxyFirst: 'Сначала добавьте прокси',
    added: 'Добавлено',
    proxySaved: 'Прокси сохранён',
    confirmDeleteServer: 'Удалить все прокси, полученные с сервера?',
    confirmDelete: 'Удалить «{0}»?',
    listUpdated: 'Список обновлён',
    reconnecting: 'Переподключение…',
    reconnectForMode: 'Переподключитесь, чтобы применить режим',
    killSwitch: 'Kill switch — блокировать интернет без VPN',
    ksHintDesktop: 'Пока VPN включён, трафик может идти только через него. Если приложение упадёт или ядро остановится, интернет останется заблокированным, пока вы не подключитесь снова или не нажмёте «Разблокировать». Работает в режиме «Весь трафик».',
    ksHintProxyMode: 'Kill switch работает только в режиме «Весь трафик (TUN)».',
    ksHintAndroid: 'Если VPN не смог запуститься, интернет блокируется, а не идёт мимо прокси. Чтобы защититься и при закрытии приложения системой, включите в настройках Android «Постоянная VPN» и «Блокировать соединения без VPN» для Tainavpn.',
    vpnSettings: 'Открыть настройки VPN Android',
    blocked: 'Интернет заблокирован',
    blockedHint: 'VPN отключился — kill switch не пропускает трафик мимо прокси',
    unblock: 'Разблокировать интернет',
    ping: '⚡ Пинг',
    pingTitle: 'Проверить задержку всех прокси',
    ms: 'мс',
    update: 'Обновить',
    checkUpdates: 'Проверить обновления',
    updateAvailable: 'Доступна версия {0}',
    updateDownloading: 'Загрузка обновления… {0}%',
    updateInstalling: 'Установка…',
    updateFailed: 'Не удалось обновить: {0}',
    upToDate: 'У вас последняя версия',
    updateCheckFailed: 'Не удалось проверить обновления: {0}',
  },
  en: {
    connect: 'Connect',
    disconnected: 'Disconnected',
    connected: 'Connected',
    connecting: 'Connecting…',
    disconnecting: 'Disconnecting…',
    error: 'Error',
    proxies: 'Proxies',
    add: '+ Add',
    noProxiesYet: 'No proxies yet.',
    getLinkInBot: 'Get your subscription link from our Telegram bot and add it with “+ Add”.',
    openBot: 'Open the Telegram bot',
    addByLinkShort: 'Add by link',
    tabByLink: 'By link',
    pasteLinkHint: 'Paste the subscription link from the Telegram bot',
    addProxy: 'Add proxy',
    tabManual: 'My own proxy',
    subUrlPh: 'https://…/sub/key',
    addByLink: 'Add by link',
    manualLinkPh: 'user:password@host:port',
    orFillFields: 'or fill in the fields',
    namePh: 'Name (optional)',
    hostPh: 'Host / IP',
    portPh: 'Port',
    userPh: 'Username (if any)',
    passPh: 'Password',
    save: 'Save',
    settings: 'Settings',
    serverAddress: 'Tainavpn server address',
    mode: 'Mode',
    modeTun: 'All traffic (TUN)',
    modeProxy: 'System proxy',
    modeHintTunWin: 'All device traffic. Requires administrator rights.',
    modeHintTunLinux: 'All device traffic. Requires root / CAP_NET_ADMIN.',
    modeHintProxy: 'Only apps that use the system proxy (browsers etc.). No special rights needed.',
    bypassLan: 'Bypass local network',
    language: 'Language',
    langAuto: 'System default',
    coreLogs: 'Core logs',
    close: 'Close',
    refreshList: 'Refresh list from server',
    delete: 'Delete',
    ownProxy: 'own proxy',
    serverFallback: 'Server',
    pleaseWait: 'Please wait…',
    detectingCountry: 'Detecting country…',
    errNoConnection: 'Cannot reach the server: {0}',
    errBadResponse: 'Invalid server response (HTTP {0})',
    errEmptySub: 'The subscription has no proxies',
    errLinkHttps: 'The link must start with https://',
    errHostPort: 'Enter a valid host and port',
    addProxyFirst: 'Add a proxy first',
    added: 'Added',
    proxySaved: 'Proxy saved',
    confirmDeleteServer: 'Delete all proxies received from the server?',
    confirmDelete: 'Delete “{0}”?',
    listUpdated: 'List updated',
    reconnecting: 'Reconnecting…',
    reconnectForMode: 'Reconnect to apply the mode',
    killSwitch: 'Kill switch — block the internet without VPN',
    ksHintDesktop: 'While the VPN is on, traffic can only go through it. If the app crashes or the core stops, the internet stays blocked until you reconnect or press “Unblock”. Works in “All traffic” mode.',
    ksHintProxyMode: 'The kill switch works only in “All traffic (TUN)” mode.',
    ksHintAndroid: 'If the VPN fails to start, the internet is blocked instead of bypassing the proxy. To be protected even when the system closes the app, enable “Always-on VPN” and “Block connections without VPN” for Tainavpn in Android settings.',
    vpnSettings: 'Open Android VPN settings',
    blocked: 'Internet blocked',
    blockedHint: 'The VPN dropped — the kill switch doesn’t let traffic bypass the proxy',
    unblock: 'Unblock the internet',
    ping: '⚡ Ping',
    pingTitle: 'Check the delay of all proxies',
    ms: 'ms',
    update: 'Update',
    checkUpdates: 'Check for updates',
    updateAvailable: 'Version {0} is available',
    updateDownloading: 'Downloading the update… {0}%',
    updateInstalling: 'Installing…',
    updateFailed: 'Update failed: {0}',
    upToDate: 'You have the latest version',
    updateCheckFailed: 'Could not check for updates: {0}',
  },
};

let LANG = 'ru';

function detectLang() {
  const langs = (typeof navigator !== 'undefined' && (navigator.languages || [navigator.language])) || [];
  for (const l of langs) {
    const code = String(l || '').slice(0, 2).toLowerCase();
    if (I18N[code]) return code;
  }
  return 'en';
}

// setLang('auto' | 'ru' | 'en')
function setLang(pref) {
  LANG = pref && pref !== 'auto' && I18N[pref] ? pref : detectLang();
  if (typeof document !== 'undefined') {
    document.documentElement.lang = LANG;
    applyI18n(document);
  }
  return LANG;
}

function t(key, ...args) {
  let s = (I18N[LANG] && I18N[LANG][key]) || I18N.en[key] || key;
  args.forEach((a, i) => { s = s.replace('{' + i + '}', a); });
  return s;
}

// Fills elements marked with data-i18n (text), data-i18n-ph (placeholder),
// data-i18n-title (title) and data-i18n-aria (aria-label).
function applyI18n(root) {
  root.querySelectorAll('[data-i18n]').forEach((el) => { el.textContent = t(el.dataset.i18n); });
  root.querySelectorAll('[data-i18n-ph]').forEach((el) => { el.placeholder = t(el.dataset.i18nPh); });
  root.querySelectorAll('[data-i18n-title]').forEach((el) => { el.title = t(el.dataset.i18nTitle); });
  root.querySelectorAll('[data-i18n-aria]').forEach((el) => { el.setAttribute('aria-label', t(el.dataset.i18nAria)); });
}

if (typeof module !== 'undefined') module.exports = { I18N, t, setLang };
