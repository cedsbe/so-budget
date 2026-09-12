package web

import (
	"errors"
	"net/http"

	"github.com/cedsbe/so-budget/internal/service"
)

type loginData struct{ Error string }

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "login.html", loginData{})
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	p, err := s.svc.Login(r.Context(), r.FormValue("name"), r.FormValue("password"))
	if errors.Is(err, service.ErrInvalidCredentials) {
		s.render(w, r, "login.html", loginData{Error: "Invalid name or password."})
		return
	}
	if err != nil {
		httpError(w, err)
		return
	}
	sess := s.sessions.Create(p)
	s.setCookie(w, sess.ID)
	s.afterLogin(r, sess) // Task 12 makes this run the first sync.
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// afterLogin is a hook replaced in Task 12.
func (s *Server) afterLogin(r *http.Request, sess *Session) {}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if sess := s.session(r); sess != nil {
		s.sessions.Delete(sess.ID)
	}
	s.clearCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

type inviteData struct {
	Name, Token, Error, RecoveryCode string
}

func (s *Server) invitePage(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	name, err := s.svc.InviteName(r.Context(), tok)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.render(w, r, "invite.html", inviteData{Name: name, Token: tok})
}

func (s *Server) inviteSubmit(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	name, err := s.svc.InviteName(r.Context(), tok)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	pw, confirm := r.FormValue("password"), r.FormValue("confirm")
	if pw != confirm {
		s.render(w, r, "invite.html", inviteData{Name: name, Token: tok, Error: "Passwords do not match."})
		return
	}
	code, err := s.svc.Activate(r.Context(), tok, pw)
	if errors.Is(err, service.ErrWeakPassword) {
		s.render(w, r, "invite.html", inviteData{Name: name, Token: tok, Error: err.Error()})
		return
	}
	if err != nil {
		httpError(w, err)
		return
	}
	s.render(w, r, "invite.html", inviteData{Name: name, RecoveryCode: code})
}

type recoverData struct{ Error, RecoveryCode string }

func (s *Server) recoverPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "recovery.html", recoverData{})
}

func (s *Server) recoverSubmit(w http.ResponseWriter, r *http.Request) {
	code, err := s.svc.Recover(r.Context(), r.FormValue("name"), r.FormValue("code"), r.FormValue("password"))
	if errors.Is(err, service.ErrInvalidCredentials) || errors.Is(err, service.ErrWeakPassword) {
		s.render(w, r, "recovery.html", recoverData{Error: err.Error()})
		return
	}
	if err != nil {
		httpError(w, err)
		return
	}
	s.render(w, r, "recovery.html", recoverData{RecoveryCode: code})
}
