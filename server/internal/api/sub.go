package api

import (
	"net/http"

	"github.com/nshimanovskiy/tainavpn/server/internal/pool"
	"github.com/nshimanovskiy/tainavpn/server/internal/store"
)

// resolve finds the user for a subscription key and returns their proxies.
func (a *API) resolve(w http.ResponseWriter, r *http.Request) (store.User, []pool.Proxy, bool) {
	u, ok := a.store.ByKey(r.PathValue("key"), clientIP(r))
	if !ok {
		errJSON(w, 404, msg(r, "unknownKey"))
		return u, nil, false
	}
	if u.Disabled {
		errJSON(w, 403, msg(r, "keyDisabled"))
		return u, nil, false
	}
	list, err := a.assign(u, false)
	if err != nil {
		errJSON(w, 503, msg(r, "noProxies"))
		return u, nil, false
	}
	return u, list, true
}
