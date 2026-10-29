package app

import (
	"net/http"
	"strconv"
)

func (a *App) resolveAI(w http.ResponseWriter, r *http.Request) {
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, 404, "找不到客服單。")
		return
	}
	version, err := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if err != nil || version < 1 {
		a.problem(w, r, 400, "表單已失效，請重新載入客服單。")
		return
	}
	if err = a.Store.ResolveAI(r.Context(), current(r).User, id, version); err != nil {
		a.fail(w, r, err)
		return
	}
	a.afterMutation(w, r, id, false)
}
