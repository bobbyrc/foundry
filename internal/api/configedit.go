package api

import (
	"fmt"
	"net/http"
	"os"

	"github.com/bobbyrc/onlet/internal/config"
)

// Config editing is owner-only (see apiRoutes). The file is the source of truth: every save
// writes the file, and the daemon then reloads it. See docs/proposals/accounts-and-config-editor.md.

// configFile is the body of GET /config/file.
type configFile struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Text    string `json:"text"`
}

// configDocument is the body of GET /config/document. The document is the file as written, before
// ${NAME} expansion, so references show as written and never as their values.
type configDocument struct {
	Path     string            `json:"path"`
	Version  string            `json:"version"`
	Document any               `json:"document"`
	Schema   config.SchemaNode `json:"schema"`
}

// configSaved is the body of PUT /config/file and PATCH /config/document.
type configSaved struct {
	Version         string   `json:"version"`
	RestartRequired []string `json:"restart_required"`
}

// readConfigFile returns the config file's path and current bytes.
func (s *server) readConfigFile() (string, []byte, error) {
	path := s.opts.Controller.Config().Path
	text, err := os.ReadFile(path)
	if err != nil {
		return path, nil, fmt.Errorf("read config: %w", err)
	}
	return path, text, nil
}

func (s *server) getConfigFile(w http.ResponseWriter, r *http.Request) {
	path, text, err := s.readConfigFile()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, configFile{Path: path, Version: config.Version(text), Text: string(text)})
}

func (s *server) getConfigDocument(w http.ResponseWriter, r *http.Request) {
	path, text, err := s.readConfigFile()
	if err != nil {
		s.fail(w, err)
		return
	}
	doc, err := config.DocumentTree(text)
	if err != nil {
		writeErrorProblems(w, http.StatusUnprocessableEntity, "the config is not valid YAML", config.ProblemsFrom(err))
		return
	}
	writeJSON(w, http.StatusOK, configDocument{
		Path:     path,
		Version:  config.Version(text),
		Document: doc,
		Schema:   config.Schema(),
	})
}

func (s *server) putConfigFile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version string  `json:"version"`
		Text    *string `json:"text"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if in.Text == nil {
		s.fail(w, invalidf("text is required"))
		return
	}
	text := []byte(*in.Text)
	s.saveConfig(w, r, in.Version, func([]byte) ([]byte, error) { return text, nil })
}

func (s *server) patchConfigDocument(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Version string          `json:"version"`
		Changes []config.Change `json:"changes"`
	}
	if err := readJSON(w, r, &in); err != nil {
		s.fail(w, err)
		return
	}
	s.saveConfig(w, r, in.Version, func(current []byte) ([]byte, error) {
		return config.ApplyChanges(current, in.Changes)
	})
}

// saveConfig is the one path every config save takes. It refuses a save based on a version other
// than the file's current one, builds the new text with edit, validates it exactly as a load does,
// writes it atomically and reloads the daemon. Saves run one at a time.
func (s *server) saveConfig(w http.ResponseWriter, r *http.Request, version string, edit func(current []byte) ([]byte, error)) {
	s.configMu.Lock()
	defer s.configMu.Unlock()

	path, current, err := s.readConfigFile()
	if err != nil {
		s.fail(w, err)
		return
	}
	if config.Version(current) != version {
		writeErrorMsg(w, http.StatusConflict, "the config file changed since you opened it; reload and try again")
		return
	}
	text, err := edit(current)
	if err != nil {
		writeErrorMsg(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	cfg, err := config.Parse(text, path)
	if err != nil {
		writeErrorProblems(w, http.StatusUnprocessableEntity, "the config is not valid", config.ProblemsFrom(err))
		return
	}
	old := s.opts.Controller.Config()
	if err := config.WriteFileAtomic(path, text); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.opts.Controller.ReloadConfig(r.Context()); err != nil {
		s.log.Error("api: reload after config save", "err", err)
		writeErrorMsg(w, http.StatusInternalServerError,
			"the config was saved, but the daemon could not reload it: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, configSaved{
		Version:         config.Version(text),
		RestartRequired: nonNil(config.RestartRequired(old, cfg)),
	})
}
