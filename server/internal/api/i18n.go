package api

import (
	"net/http"
	"strings"
)

// Messages shown to app users, in the language the app asks for (?lang=ru|en).
var messages = map[string][2]string{ // key -> {ru, en}
	"regClosed":   {"Регистрация закрыта, попросите ключ у администратора", "Registration is closed, ask the administrator for a key"},
	"regTooMany":  {"Слишком много регистраций с этого IP, попробуйте завтра", "Too many registrations from this IP, try tomorrow"},
	"tooMany":     {"Слишком много запросов, попробуйте позже", "Too many requests, try again later"},
	"noHost":      {"Не удалось найти хост прокси", "Cannot resolve the proxy host"},
	"localAddr":   {"Локальные адреса не проверяются", "Local addresses are not checked"},
	"proxyDown":   {"Прокси не отвечает: ", "The proxy does not respond: "},
	"unknownKey":  {"Ключ не найден", "Unknown key"},
	"keyDisabled": {"Ключ отключён администратором", "The key is disabled by the administrator"},
	"noProxies":   {"На сервере сейчас нет свободных прокси, попробуйте позже", "No proxies are available on the server right now, try again later"},
}

// msg returns a message in the request's language (Russian unless ?lang=en).
func msg(r *http.Request, key string) string {
	m, ok := messages[key]
	if !ok {
		return key
	}
	if strings.HasPrefix(strings.ToLower(r.URL.Query().Get("lang")), "en") {
		return m[1]
	}
	return m[0]
}
