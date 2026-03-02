package api

import "net/http"

func (a *API) withOpenAPIValidation(next http.HandlerFunc) http.HandlerFunc {
	if a.openAPIValidate == nil {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		a.openAPIValidate(http.HandlerFunc(next)).ServeHTTP(w, r)
	}
}
