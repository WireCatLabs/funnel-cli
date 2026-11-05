package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

type portKey struct{}

// newProxy forwards to 127.0.0.1 only, shaped like tailscale serve's proxy (ipn/ipnlocal/serve.go) so the MCP
// servers see the same request they saw behind the system Tailscale: public Host, full path, X-Forwarded-*.
func newProxy(routes func() []Route) http.Handler {
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			port := pr.In.Context().Value(portKey{}).(int)
			pr.SetURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)})
			pr.Out.Host = pr.In.Host
			for name := range pr.Out.Header {
				if strings.HasPrefix(strings.ToLower(name), "tailscale-") {
					pr.Out.Header.Del(name)
				}
			}
			pr.SetXForwarded()
			pr.Out.Header.Set("X-Forwarded-Proto", "https")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, _ error) {
			port := r.Context().Value(portKey{}).(int)
			http.Error(w, fmt.Sprintf("the local server on port %d does not answer", port), http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, ok := match(routes(), r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		rp.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), portKey{}, route.Port)))
	})
}
