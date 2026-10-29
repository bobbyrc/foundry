package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bobbyrc/onett/internal/auth"
)

// apiRoutes mounts the routes that need a principal. Each route names the action it needs
// through read, operate or administer; see those helpers and auth.Action.
func (s *server) apiRoutes(r chi.Router) {
	s.read(r, "/events", s.events)
	s.read(r, "/events/history", s.eventHistory)
	s.read(r, "/stats", s.stats)

	s.read(r, "/control", s.getControl)
	s.operate(r, http.MethodPost, "/control/pause", s.setPaused(true))
	s.operate(r, http.MethodPost, "/control/resume", s.setPaused(false))

	s.read(r, "/projects", s.listProjects)
	s.operate(r, http.MethodPost, "/projects/{name}/pause", s.setProjectPaused(true))
	s.operate(r, http.MethodPost, "/projects/{name}/resume", s.setProjectPaused(false))
	s.read(r, "/agents", s.listAgents)

	s.read(r, "/tasks", s.listTasks)
	s.operate(r, http.MethodPost, "/tasks", s.createTask)
	s.read(r, "/tasks/{id}", s.getTask)
	s.operate(r, http.MethodPost, "/tasks/{id}/cancel", s.cancelTask)
	s.operate(r, http.MethodPost, "/tasks/{id}/retry", s.retryTask)

	s.read(r, "/runs", s.listRuns)
	s.read(r, "/runs/{id}", s.getRun)
	s.read(r, "/runs/{id}/logs", s.runLogs)
	s.operate(r, http.MethodPost, "/runs/{id}/cancel", s.cancelRun)

	s.read(r, "/approvals", s.listApprovals)
	s.operate(r, http.MethodPost, "/approvals/{id}/decide", s.decideApproval)

	s.read(r, "/deployments", s.listDeployments)
	s.read(r, "/signals", s.listSignals)
	s.read(r, "/friction", s.listFriction)

	s.read(r, "/loops", s.listLoops)
	s.operate(r, http.MethodPost, "/loops/{name}/run", s.runLoop)

	s.read(r, "/config", s.getConfig)
	s.operate(r, http.MethodPost, "/config/reload", s.reloadConfig)
	s.administer(r, http.MethodGet, "/config/file", s.getConfigFile)
	s.administer(r, http.MethodPut, "/config/file", s.putConfigFile)
	s.administer(r, http.MethodGet, "/config/document", s.getConfigDocument)
	s.administer(r, http.MethodPatch, "/config/document", s.patchConfigDocument)

	s.administer(r, http.MethodGet, "/users", s.listUsers)
	s.administer(r, http.MethodPost, "/users", s.createUser)
	s.administer(r, http.MethodPost, "/users/{name}/role", s.setUserRole)
	s.administer(r, http.MethodPost, "/users/{name}/password", s.setUserPassword)
	s.administer(r, http.MethodDelete, "/users/{name}", s.deleteUser)

	s.read(r, "/conversations", s.listConversations)
	s.operate(r, http.MethodPost, "/conversations", s.createConversation)
	s.read(r, "/conversations/{id}", s.getConversation)
	s.operate(r, http.MethodPost, "/conversations/{id}/messages", s.sendMessage)
}

// read, operate and administer mount a handler under the action it needs. Every route in
// apiRoutes goes through one of them, so a route cannot be added without choosing its action.
func (s *server) read(r chi.Router, path string, h http.HandlerFunc) {
	r.With(s.need(auth.Read)).Get(path, h)
}

func (s *server) operate(r chi.Router, method, path string, h http.HandlerFunc) {
	r.With(s.need(auth.Operate)).Method(method, path, h)
}

func (s *server) administer(r chi.Router, method, path string, h http.HandlerFunc) {
	r.With(s.need(auth.Administer)).Method(method, path, h)
}
