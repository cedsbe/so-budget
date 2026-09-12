// Package web is the HTML adapter: handlers, sessions and templates.
package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/service"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

type Config struct {
	BaseURL         string
	Dev             bool
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
}

type Server struct {
	svc      *service.Service
	cfg      Config
	sessions *SessionStore
	pages    map[string]*template.Template
	mux      *http.ServeMux
}

type ctxKey int

const sessionKey ctxKey = 1

var funcs = template.FuncMap{
	"money": func(c domain.Cents) string { return c.String() },
	"date":  func(t time.Time) string { return t.Format("2006-01-02") },
	"month": func(m domain.Month) string { return m.String() },
	"lower": strings.ToLower,
	"seq": func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i + 1
		}
		return out
	},
}

func New(svc *service.Service, cfg Config) (*Server, error) {
	s := &Server{svc: svc, cfg: cfg, sessions: NewSessionStore(cfg.IdleTimeout, cfg.AbsoluteTimeout), pages: map[string]*template.Template{}}
	entries, err := fs.ReadDir(templateFS, "templates")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name() == "layout.html" {
			continue
		}
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("template %s: %w", e.Name(), err)
		}
		s.pages[e.Name()] = t
	}
	s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux = http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))

	s.mux.HandleFunc("GET /login", s.loginPage)
	s.mux.HandleFunc("POST /login", s.loginSubmit)
	s.mux.HandleFunc("GET /invite/{token}", s.invitePage)
	s.mux.HandleFunc("POST /invite/{token}", s.inviteSubmit)
	s.mux.HandleFunc("GET /recover", s.recoverPage)
	s.mux.HandleFunc("POST /recover", s.recoverSubmit)

	s.private("POST /logout", s.logout)
	s.routesInbox() // Task 12
	s.routesLedger()
	s.routesImport()   // Task 14
	s.routesPlanning() // Task 15
	s.routesReports()  // Task 16
}

// private registers a handler that requires a session and, for non-GET, a CSRF token.
func (s *Server) private(pattern string, h http.HandlerFunc) {
	s.mux.Handle(pattern, s.requireSession(s.checkCSRF(h)))
}

// Stubs so the file compiles before later tasks add routes.
func (s *Server) routesImport()   {}
func (s *Server) routesPlanning() {}
func (s *Server) routesReports()  {}

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("sb_session")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		sess, ok := s.sessions.Get(c.Value)
		if !ok {
			s.clearCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, sess)))
	})
}

func (s *Server) checkCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			sess := s.session(r)
			tok := r.Header.Get("X-CSRF-Token")
			if tok == "" {
				tok = r.FormValue("csrf")
			}
			if sess == nil || tok == "" || tok != sess.CSRF {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) session(r *http.Request) *Session {
	sess, _ := r.Context().Value(sessionKey).(*Session)
	return sess
}

func (s *Server) setCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{Name: "sb_session", Value: id, Path: "/", HttpOnly: true, Secure: !s.cfg.Dev, SameSite: http.SameSiteLaxMode})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "sb_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: !s.cfg.Dev, SameSite: http.SameSiteLaxMode})
}

type page struct {
	Title string
	User  *domain.User
	CSRF  string
	Flash string
	Path  string
	Data  any
}

// render executes a page template inside the layout. The session, if any, supplies user, CSRF and flash.
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data any) {
	t, ok := s.pages[name]
	if !ok {
		http.Error(w, "missing template "+name, http.StatusInternalServerError)
		return
	}
	p := page{Title: "so-budget", Path: r.URL.Path, Data: data}
	if sess := s.session(r); sess != nil {
		p.User = &domain.User{ID: sess.P.UserID, Name: sess.P.Name}
		p.CSRF = sess.CSRF
		p.Flash, sess.Flash = sess.Flash, ""
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout.html", p); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

// renderPartial executes a named block without the layout (for htmx swaps).
func (s *Server) renderPartial(w http.ResponseWriter, r *http.Request, file, block string, data any) {
	t, ok := s.pages[file]
	if !ok {
		http.Error(w, "missing template "+file, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	p := page{Data: data}
	if sess := s.session(r); sess != nil {
		p.CSRF = sess.CSRF
		p.User = &domain.User{ID: sess.P.UserID, Name: sess.P.Name}
	}
	if err := t.ExecuteTemplate(w, block, p); err != nil {
		log.Printf("render %s#%s: %v", file, block, err)
	}
}

func (s *Server) flash(r *http.Request, msg string) {
	if sess := s.session(r); sess != nil {
		sess.Flash = msg
	}
}

// httpError maps service errors to status codes.
func httpError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, service.ErrInvalidState), errors.Is(err, service.ErrInvalidAmount), errors.Is(err, service.ErrNotLinked):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		log.Printf("error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
