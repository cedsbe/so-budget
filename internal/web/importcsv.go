package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/service"
)

func (s *Server) routesImport() {
	s.private("GET /settings/import", s.importPage)
	s.private("POST /settings/import/preview", s.importPreview)
	s.private("POST /settings/import/commit", s.importCommit)
}

type importData struct {
	Preview    *service.ImportPreview
	Users      []domain.User
	Categories []domain.Category
	Error      string
}

func (s *Server) importPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "import.html", importData{})
}

func (s *Server) importPreview(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		s.render(w, r, "import.html", importData{Error: "Upload failed: " + err.Error()})
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		s.render(w, r, "import.html", importData{Error: "Choose a CSV file."})
		return
	}
	defer f.Close()
	p, err := service.ParseImportCSV(f)
	if err != nil {
		s.render(w, r, "import.html", importData{Error: err.Error()})
		return
	}
	s.session(r).Import = &p
	users, _ := s.svc.Users(r.Context())
	cats, _ := s.svc.Categories(r.Context(), false)
	s.render(w, r, "import.html", importData{Preview: &p, Users: users, Categories: cats})
}

func (s *Server) importCommit(w http.ResponseWriter, r *http.Request) {
	sess := s.session(r)
	p, _ := sess.Import.(*service.ImportPreview)
	if p == nil {
		s.render(w, r, "import.html", importData{Error: "Nothing to import; upload a file first."})
		return
	}
	m := service.ImportMapping{Payers: map[string]int64{}, Types: map[string]int64{}, Negate: r.FormValue("negate") != ""}
	for key, vals := range r.Form {
		if len(vals) == 0 {
			continue
		}
		id, _ := strconv.ParseInt(vals[0], 10, 64)
		switch {
		case strings.HasPrefix(key, "payer."):
			m.Payers[strings.TrimPrefix(key, "payer.")] = id
		case strings.HasPrefix(key, "type."):
			m.Types[strings.TrimPrefix(key, "type.")] = id
		}
	}
	n, err := s.svc.CommitImport(r.Context(), *p, m)
	if err != nil {
		users, _ := s.svc.Users(r.Context())
		cats, _ := s.svc.Categories(r.Context(), false)
		s.render(w, r, "import.html", importData{Preview: p, Users: users, Categories: cats, Error: err.Error()})
		return
	}
	sess.Import = nil
	s.flash(r, strconv.Itoa(n)+" entries imported.")
	http.Redirect(w, r, "/ledger", http.StatusSeeOther)
}
