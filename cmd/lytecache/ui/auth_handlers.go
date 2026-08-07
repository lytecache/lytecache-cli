package ui

import (
	"crypto/subtle"
	"fmt"
	"net/http"
)

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if _, ok := s.sessions.verify(cookie.Value); ok {
			http.Redirect(w, r, "/dashboard", http.StatusFound)
			return
		}
	}
	s.writeTemplate(w, loginTmpl, loginPage{})
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if allowed, retryAfter := s.rateLimiter.allow(ip); !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
		s.writeLoginError(w, http.StatusTooManyRequests, "too many attempts -- try again shortly")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.writeBadRequest(w, r, err.Error())
		return
	}
	username := r.PostForm.Get("username")
	password := r.PostForm.Get("password")

	s.authMu.Lock()
	cfg := s.authConfig
	s.authMu.Unlock()

	// VerifyPassword always runs, regardless of whether username matched,
	// so a wrong username and a wrong password take the same amount of
	// time -- a single-admin model has little to enumerate, but this
	// costs nothing and avoids a lazy shortcut creeping in later.
	validPassword := VerifyPassword(password, cfg.PasswordHash)
	validUsername := subtle.ConstantTimeCompare([]byte(username), []byte(cfg.Username)) == 1
	if !validUsername || !validPassword {
		s.rateLimiter.recordFailure(ip)
		s.writeLoginError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	s.rateLimiter.recordSuccess(ip)
	if err := s.sessions.issue(w, username); err != nil {
		s.writeServerError(w, err)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (s *Server) writeLoginError(w http.ResponseWriter, status int, message string) {
	body, err := render(loginTmpl, loginPage{Error: message})
	if err != nil {
		s.writeServerError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if claims, ok := s.currentSession(r); ok {
		s.sessions.revoke(claims)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) handleChangePasswordForm(w http.ResponseWriter, r *http.Request) {
	s.writeTemplate(w, changePasswordTmpl, changePasswordPage{
		Page:   s.basePage(r),
		Forced: s.forcedPasswordChangeRequired(),
	})
}

func (s *Server) handleChangePasswordSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeBadRequest(w, r, err.Error())
		return
	}
	newPassword := r.PostForm.Get("new_password")

	s.authMu.Lock()
	cfg := s.authConfig
	s.authMu.Unlock()

	page := changePasswordPage{Page: s.basePage(r), Forced: s.forcedPasswordChangeRequired()}
	switch {
	case len(newPassword) < 8:
		page.Error = "password must be at least 8 characters"
	case VerifyPassword(newPassword, cfg.PasswordHash):
		page.Error = "new password must be different from the current one"
	}
	if page.Error != "" {
		s.writeTemplate(w, changePasswordTmpl, page)
		return
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		s.writeServerError(w, err)
		return
	}

	s.authMu.Lock()
	s.authConfig.PasswordHash = hash
	saveErr := SaveAuthConfig(s.authConfigPath, s.authConfig)
	s.authMu.Unlock()
	if saveErr != nil {
		s.writeServerError(w, saveErr)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusFound)
}
