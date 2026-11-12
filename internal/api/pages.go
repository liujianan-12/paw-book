package api

import (
	"embed"
	"net/http"
)

//go:embed web/*
var webFS embed.FS

// pages 提供四角色最小页面（E3：真实调接口并展示失败原因）。
func (s *Server) pages() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/index.html":
			http.Redirect(w, r, "/login.html", http.StatusFound)
			return
		case "/login.html", "/user.html", "/reception.html", "/service.html", "/cs.html", "/manager.html":
			data, err := webFS.ReadFile("web" + r.URL.Path)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(data)
			return
		case "/app.css":
			data, err := webFS.ReadFile("web/app.css")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			_, _ = w.Write(data)
			return
		case "/app.js":
			data, err := webFS.ReadFile("web/app.js")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	})
}
