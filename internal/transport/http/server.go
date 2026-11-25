package httptransport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"door/internal/app"
	"door/internal/auth"
	"door/internal/config"
	"door/internal/database"
	"door/internal/monitor"
	"door/internal/nginx"
	"door/internal/settings"
	"door/internal/sys"
	"door/internal/task"
	"door/internal/terminal"
)

const sessionCookieName = "door_session"

type contextKey string

const userContextKey contextKey = "door_user"

// Server serves Door's REST API, SSE/WebSocket streams, and embedded SPA frontend.
type Server struct {
	logger   *slog.Logger
	cfg      config.Config
	auth     *auth.Service
	settings *settings.Service
	monitor  *monitor.Service
	tasks    *task.Queue
	driver   sys.Driver
	apps     *app.Service
	nginx    *nginx.Service
	dbSvc    *database.Service
	term     *terminal.Handler
	webFS    fs.FS
	mux      *http.ServeMux
}

// Deps bundles dependencies for constructing the HTTP Server.
type Deps struct {
	Logger   *slog.Logger
	Config   config.Config
	Auth     *auth.Service
	Settings *settings.Service
	Monitor  *monitor.Service
	Tasks    *task.Queue
	Driver   sys.Driver
	Apps     *app.Service
	Nginx    *nginx.Service
	Database *database.Service
	Terminal *terminal.Handler
	WebFS    fs.FS
}

// NewServer constructs and wires all HTTP routes and middlewares.
func NewServer(d Deps) *Server {
	s := &Server{
		logger:   d.Logger,
		cfg:      d.Config,
		auth:     d.Auth,
		settings: d.Settings,
		monitor:  d.Monitor,
		tasks:    d.Tasks,
		driver:   d.Driver,
		apps:     d.Apps,
		nginx:    d.Nginx,
		dbSvc:    d.Database,
		term:     d.Terminal,
		webFS:    d.WebFS,
		mux:      http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

// Handler returns the root HTTP handler wrapped in security, recovery, and logging middlewares.
func (s *Server) Handler() http.Handler {
	return s.recoverMiddleware(s.securityHeaders(s.requestLogger(s.mux)))
}

func (s *Server) registerRoutes() {
	// Public routes
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)

	// Authenticated routes allowed even before mandatory initial password change
	s.mux.Handle("GET /api/v1/auth/me", s.requireAuth(false, http.HandlerFunc(s.handleMe)))
	s.mux.Handle("POST /api/v1/auth/change-password", s.requireAuth(false, http.HandlerFunc(s.handleChangePassword)))

	// Monitor, Settings, Menus, Tasks, Audit
	s.mux.Handle("GET /api/v1/monitor/snapshot", s.requireAuth(true, http.HandlerFunc(s.handleMonitorSnapshot)))
	s.mux.Handle("GET /api/v1/settings", s.requireAuth(true, http.HandlerFunc(s.handleGetSettings)))
	s.mux.Handle("PUT /api/v1/settings", s.requireAuth(true, http.HandlerFunc(s.handleUpdateSettings)))
	s.mux.Handle("GET /api/v1/menus", s.requireAuth(true, http.HandlerFunc(s.handleGetMenus)))
	s.mux.Handle("PUT /api/v1/menus/reorder", s.requireAuth(true, http.HandlerFunc(s.handleReorderMenus)))
	s.mux.Handle("POST /api/v1/menus/reset", s.requireAuth(true, http.HandlerFunc(s.handleResetMenus)))
	s.mux.Handle("PUT /api/v1/menus/{id}", s.requireAuth(true, http.HandlerFunc(s.handleUpdateMenu)))
	s.mux.Handle("GET /api/v1/tasks", s.requireAuth(true, http.HandlerFunc(s.handleListTasks)))
	s.mux.Handle("GET /api/v1/tasks/{id}", s.requireAuth(true, http.HandlerFunc(s.handleGetTask)))
	s.mux.Handle("GET /api/v1/tasks/{id}/logs", s.requireAuth(true, http.HandlerFunc(s.handleTaskLogs)))
	s.mux.Handle("GET /api/v1/tasks/{id}/stream", s.requireAuth(true, http.HandlerFunc(s.handleTaskStream)))
	s.mux.Handle("GET /api/v1/audit-logs", s.requireAuth(true, http.HandlerFunc(s.handleListAuditLogs)))

	// Multi-language Runtimes & Projects (Go / Java / Python)
	if s.apps != nil {
		s.mux.Handle("GET /api/v1/runtimes", s.requireAuth(true, http.HandlerFunc(s.handleListRuntimes)))
		s.mux.Handle("POST /api/v1/runtimes/install", s.requireAuth(true, http.HandlerFunc(s.handleInstallRuntime)))
		s.mux.Handle("DELETE /api/v1/runtimes/{id}", s.requireAuth(true, http.HandlerFunc(s.handleDeleteRuntime)))
		s.mux.Handle("GET /api/v1/projects", s.requireAuth(true, http.HandlerFunc(s.handleListProjects)))
		s.mux.Handle("POST /api/v1/projects", s.requireAuth(true, http.HandlerFunc(s.handleCreateProject)))
		s.mux.Handle("POST /api/v1/projects/{id}/action", s.requireAuth(true, http.HandlerFunc(s.handleProjectAction)))
		s.mux.Handle("GET /api/v1/projects/{id}/logs", s.requireAuth(true, http.HandlerFunc(s.handleProjectLogs)))
		s.mux.Handle("DELETE /api/v1/projects/{id}", s.requireAuth(true, http.HandlerFunc(s.handleDeleteProject)))
	}

	// Nginx & SSL Management
	if s.nginx != nil {
		s.mux.Handle("GET /api/v1/nginx/info", s.requireAuth(true, http.HandlerFunc(s.handleNginxInfo)))
		s.mux.Handle("POST /api/v1/nginx/install", s.requireAuth(true, http.HandlerFunc(s.handleNginxInstall)))
		s.mux.Handle("POST /api/v1/nginx/action", s.requireAuth(true, http.HandlerFunc(s.handleNginxAction)))
		s.mux.Handle("PUT /api/v1/nginx/config", s.requireAuth(true, http.HandlerFunc(s.handleNginxUpdateConfig)))
		s.mux.Handle("GET /api/v1/nginx/sites", s.requireAuth(true, http.HandlerFunc(s.handleListSites)))
		s.mux.Handle("POST /api/v1/nginx/sites", s.requireAuth(true, http.HandlerFunc(s.handleCreateSite)))
		s.mux.Handle("GET /api/v1/nginx/sites/{id}/config", s.requireAuth(true, http.HandlerFunc(s.handleGetSiteConfig)))
		s.mux.Handle("PUT /api/v1/nginx/sites/{id}/config", s.requireAuth(true, http.HandlerFunc(s.handleUpdateSiteConfig)))
		s.mux.Handle("DELETE /api/v1/nginx/sites/{id}", s.requireAuth(true, http.HandlerFunc(s.handleDeleteSite)))
		s.mux.Handle("GET /api/v1/ssl/certs", s.requireAuth(true, http.HandlerFunc(s.handleListCerts)))
		s.mux.Handle("POST /api/v1/ssl/certs", s.requireAuth(true, http.HandlerFunc(s.handleIssueCert)))
		s.mux.Handle("DELETE /api/v1/ssl/certs/{id}", s.requireAuth(true, http.HandlerFunc(s.handleDeleteCert)))
	}

	// Native Databases (MySQL / PostgreSQL / Redis / MongoDB)
	if s.dbSvc != nil {
		s.mux.Handle("GET /api/v1/databases/{engine}", s.requireAuth(true, http.HandlerFunc(s.handleGetDB)))
		s.mux.Handle("POST /api/v1/databases/{engine}/install", s.requireAuth(true, http.HandlerFunc(s.handleInstallDB)))
		s.mux.Handle("POST /api/v1/databases/{engine}/action", s.requireAuth(true, http.HandlerFunc(s.handleActionDB)))
		s.mux.Handle("GET /api/v1/databases/{engine}/config", s.requireAuth(true, http.HandlerFunc(s.handleGetDBConfig)))
		s.mux.Handle("PUT /api/v1/databases/{engine}/config", s.requireAuth(true, http.HandlerFunc(s.handleUpdateDBConfig)))
		s.mux.Handle("GET /api/v1/databases/{engine}/schemas", s.requireAuth(true, http.HandlerFunc(s.handleListDBSchemas)))
		s.mux.Handle("POST /api/v1/databases/{engine}/schemas", s.requireAuth(true, http.HandlerFunc(s.handleCreateDBSchema)))
		s.mux.Handle("DELETE /api/v1/databases/{engine}/schemas/{id}", s.requireAuth(true, http.HandlerFunc(s.handleDeleteDBSchema)))
		s.mux.Handle("GET /api/v1/databases/{engine}/backups", s.requireAuth(true, http.HandlerFunc(s.handleListDBBackups)))
		s.mux.Handle("POST /api/v1/databases/{engine}/backups", s.requireAuth(true, http.HandlerFunc(s.handleCreateDBBackup)))
	}

	// Web SSH PTY Terminal
	if s.term != nil {
		s.mux.Handle("GET /api/v1/terminal/ws", s.requireAuth(true, s.term))
	}

	// Embedded SPA static assets + client-side routing fallback
	if s.webFS != nil {
		s.mux.HandleFunc("GET /", s.handleSPA)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"driver_mode": s.driver.Mode(),
		"dev":         s.cfg.Dev,
		"time":        time.Now().UTC(),
	})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}

	ip := clientIP(r)
	sess, user, err := s.auth.Login(r.Context(), req.Username, req.Password, ip, r.UserAgent())
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			s.auth.RecordAudit(r.Context(), req.Username, "login_failed", "panel", "invalid credentials", ip)
			writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "用户名或密码错误")
			return
		}
		s.logger.Error("login error", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "登录处理失败")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sess.Token,
		Path:     "/",
		Expires:  sess.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	s.auth.RecordAudit(r.Context(), user.Username, "login", "panel", "login success", ip)

	writeJSON(w, http.StatusOK, map[string]any{
		"token":      sess.Token,
		"expires_at": sess.ExpiresAt,
		"user":       user,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token != "" {
		if u, err := s.auth.Authenticate(r.Context(), token); err == nil {
			s.auth.RecordAudit(r.Context(), u.Username, "logout", "panel", "logout", clientIP(r))
		}
		_ = s.auth.Logout(r.Context(), token)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	ps, err := s.settings.GetPanelSettings(r.Context())
	panelName := "Door 运维面板"
	if err == nil && ps.PanelName != "" {
		panelName = ps.PanelName
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":        u,
		"panel_name":  panelName,
		"driver_mode": s.driver.Mode(),
		"dev":         s.cfg.Dev,
	})
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	var req changePasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}

	if err := s.auth.ChangePassword(r.Context(), u.ID, req.OldPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusBadRequest, "INVALID_OLD_PASSWORD", "原密码不正确")
		case errors.Is(err, auth.ErrWeakPassword):
			writeError(w, http.StatusBadRequest, "WEAK_PASSWORD", "新密码至少 8 位，且不能与原密码或默认密码相同")
		default:
			s.logger.Error("change password failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "修改密码失败")
		}
		return
	}

	s.auth.RecordAudit(r.Context(), u.Username, "change_password", "user:"+u.Username, "password updated", clientIP(r))
	writeJSON(w, http.StatusOK, map[string]any{
		"status":               "ok",
		"must_change_password": false,
	})
}

func (s *Server) handleMonitorSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	paths := s.cfg.Paths
	if ps, err := s.settings.GetPanelSettings(ctx); err == nil {
		if ps.ServerDir != "" {
			paths.ServerDir = ps.ServerDir
		}
		if ps.DatabaseDir != "" {
			paths.DatabaseDir = ps.DatabaseDir
		}
		if ps.ProjectDir != "" {
			paths.ProjectDir = ps.ProjectDir
		}
		if ps.NginxDir != "" {
			paths.NginxDir = ps.NginxDir
		}
	}

	snap := s.monitor.Snapshot(ctx, paths)
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	ps, err := s.settings.GetPanelSettings(r.Context())
	if err != nil {
		s.logger.Error("get settings failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "获取面板设置失败")
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	var req settings.PanelSettings
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}

	updated, err := s.settings.UpdatePanelSettings(r.Context(), req)
	if err != nil {
		if errors.Is(err, settings.ErrInvalidSetting) {
			writeError(w, http.StatusBadRequest, "INVALID_SETTING", err.Error())
			return
		}
		s.logger.Error("update settings failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "保存设置失败")
		return
	}

	s.auth.RecordAudit(r.Context(), u.Username, "update_settings", "panel_settings", fmt.Sprintf("addr=%s name=%s", updated.ListenAddr, updated.PanelName), clientIP(r))
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleGetMenus(w http.ResponseWriter, r *http.Request) {
	tree, err := s.settings.ListMenuTree(r.Context())
	if err != nil {
		s.logger.Error("list menus failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "获取菜单失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": tree})
}

func (s *Server) handleUpdateMenu(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "无效的菜单 ID")
		return
	}

	var req settings.MenuUpdateInput
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}

	node, err := s.settings.UpdateMenuItem(r.Context(), id, req)
	if err != nil {
		switch {
		case errors.Is(err, settings.ErrMenuNotFound):
			writeError(w, http.StatusNotFound, "MENU_NOT_FOUND", "菜单项不存在")
		case errors.Is(err, settings.ErrInvalidMenu):
			writeError(w, http.StatusBadRequest, "INVALID_MENU", err.Error())
		default:
			s.logger.Error("update menu failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "更新菜单失败")
		}
		return
	}

	s.auth.RecordAudit(r.Context(), u.Username, "update_menu", "menu:"+node.Key, node.Title, clientIP(r))
	writeJSON(w, http.StatusOK, node)
}

type reorderMenusRequest struct {
	Items []settings.MenuTreeInput `json:"items"`
}

func (s *Server) handleReorderMenus(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	var req reorderMenusRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}

	tree, err := s.settings.ReorderMenuTree(r.Context(), req.Items)
	if err != nil {
		switch {
		case errors.Is(err, settings.ErrMenuNotFound):
			writeError(w, http.StatusNotFound, "MENU_NOT_FOUND", err.Error())
		case errors.Is(err, settings.ErrInvalidMenu):
			writeError(w, http.StatusBadRequest, "INVALID_MENU", err.Error())
		default:
			s.logger.Error("reorder menus failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "保存菜单布局失败")
		}
		return
	}

	s.auth.RecordAudit(r.Context(), u.Username, "reorder_menus", "menus", fmt.Sprintf("top_groups=%d", len(tree)), clientIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"items": tree})
}

func (s *Server) handleResetMenus(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	tree, err := s.settings.ResetDefaultMenus(r.Context())
	if err != nil {
		s.logger.Error("reset menus failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "恢复默认菜单失败")
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "reset_menus", "menus", "restored defaults", clientIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"items": tree})
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	items, err := s.tasks.List(r.Context(), 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "读取任务列表失败")
		return
	}
	if items == nil {
		items = []task.Task{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.tasks.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, task.ErrTaskNotFound) {
		writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "读取任务失败")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleTaskLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := s.tasks.ReadLog(r.Context(), r.PathValue("id"))
	if errors.Is(err, task.ErrTaskNotFound) {
		writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "读取日志失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": logs})
}

func (s *Server) handleTaskStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.tasks.Get(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "任务不存在")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "STREAM_UNSUPPORTED", "不支持流式响应")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsub := s.tasks.Subscribe(id)
	defer unsub()

	if existing, err := s.tasks.ReadLog(r.Context(), id); err == nil && existing != "" {
		for _, line := range strings.Split(strings.TrimRight(existing, "\n"), "\n") {
			fmt.Fprintf(w, "data: %s\n\n", line)
		}
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case line, open := <-ch:
			if !open {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		}
	}
}

func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	items, err := s.auth.ListAuditLogs(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "获取审计日志失败")
		return
	}
	if items == nil {
		items = []auth.AuditLog{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// --- Runtimes & Projects ---

func (s *Server) handleListRuntimes(w http.ResponseWriter, r *http.Request) {
	items, err := s.apps.ListRuntimes(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleInstallRuntime(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	var req struct {
		Kind    string `json:"kind"`
		Version string `json:"version"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	tsk, err := s.apps.InstallRuntime(r.Context(), req.Kind, req.Version)
	if err != nil {
		switch {
		case errors.Is(err, app.ErrInvalidApp):
			writeError(w, http.StatusBadRequest, "INVALID_RUNTIME", err.Error())
		case errors.Is(err, app.ErrAlreadyExists):
			writeError(w, http.StatusConflict, "RUNTIME_EXISTS", "该版本运行环境已存在")
		default:
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		}
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "install_runtime", req.Kind+":"+req.Version, tsk.ID, clientIP(r))
	writeJSON(w, http.StatusAccepted, tsk)
}

func (s *Server) handleDeleteRuntime(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "无效的运行环境 ID")
		return
	}
	if err := s.apps.DeleteRuntime(r.Context(), id); err != nil {
		if errors.Is(err, app.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "运行环境不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	items, err := s.apps.ListProjects(r.Context(), r.URL.Query().Get("lang"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	var req app.CreateProjectInput
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	proj, err := s.apps.CreateProject(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, app.ErrInvalidApp):
			writeError(w, http.StatusBadRequest, "INVALID_PROJECT", err.Error())
		case errors.Is(err, app.ErrAlreadyExists):
			writeError(w, http.StatusConflict, "PROJECT_EXISTS", "项目名称已存在")
		default:
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		}
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "create_project", proj.Name, proj.Lang, clientIP(r))
	writeJSON(w, http.StatusCreated, proj)
}

func (s *Server) handleProjectAction(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "无效的项目 ID")
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	proj, err := s.apps.ActionProject(r.Context(), id, req.Action)
	if err != nil {
		switch {
		case errors.Is(err, app.ErrNotFound):
			writeError(w, http.StatusNotFound, "NOT_FOUND", "项目不存在")
		case errors.Is(err, app.ErrInvalidApp):
			writeError(w, http.StatusBadRequest, "INVALID_ACTION", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		}
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "project_"+req.Action, proj.Name, proj.Status, clientIP(r))
	writeJSON(w, http.StatusOK, proj)
}

func (s *Server) handleProjectLogs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "无效的项目 ID")
		return
	}
	logs, err := s.apps.ReadProjectLog(r.Context(), id)
	if err != nil {
		if errors.Is(err, app.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "项目不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": logs})
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "无效的项目 ID")
		return
	}
	if err := s.apps.DeleteProject(r.Context(), id); err != nil {
		if errors.Is(err, app.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "项目不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "delete_project", strconv.FormatInt(id, 10), "", clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Nginx & SSL ---

func (s *Server) handleNginxInfo(w http.ResponseWriter, r *http.Request) {
	info, err := s.nginx.Info(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleNginxInstall(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	tsk, err := s.nginx.InstallNginx(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "install_nginx", "nginx", tsk.ID, clientIP(r))
	writeJSON(w, http.StatusAccepted, tsk)
}

func (s *Server) handleNginxAction(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	var req struct {
		Action string `json:"action"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	info, err := s.nginx.Action(r.Context(), req.Action)
	if err != nil {
		writeError(w, http.StatusBadRequest, "NGINX_ACTION_FAILED", err.Error())
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "nginx_"+req.Action, "nginx", info.Status, clientIP(r))
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleNginxUpdateConfig(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	var req struct {
		ConfigText string `json:"config_text"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	if err := s.nginx.UpdateMainConfig(r.Context(), req.ConfigText); err != nil {
		writeError(w, http.StatusBadRequest, "NGINX_CONFIG_ERROR", err.Error())
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "update_nginx_conf", "nginx.conf", "validated", clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListSites(w http.ResponseWriter, r *http.Request) {
	items, err := s.nginx.ListSites(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	var req nginx.CreateSiteInput
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	site, err := s.nginx.CreateSite(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, nginx.ErrSiteExists):
			writeError(w, http.StatusConflict, "SITE_EXISTS", "站点域名已存在")
		case errors.Is(err, nginx.ErrInvalidNginx), errors.Is(err, nginx.ErrCertNotFound):
			writeError(w, http.StatusBadRequest, "INVALID_SITE", err.Error())
		default:
			writeError(w, http.StatusBadRequest, "SITE_CREATE_FAILED", err.Error())
		}
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "create_site", site.Domain, site.SiteType, clientIP(r))
	writeJSON(w, http.StatusCreated, site)
}

func (s *Server) handleGetSiteConfig(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "无效的站点 ID")
		return
	}
	text, err := s.nginx.ReadSiteConfig(r.Context(), id)
	if err != nil {
		if errors.Is(err, nginx.ErrSiteNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "站点不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"config_text": text})
}

func (s *Server) handleUpdateSiteConfig(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "无效的站点 ID")
		return
	}
	var req struct {
		ConfigText string `json:"config_text"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	if err := s.nginx.UpdateSiteConfig(r.Context(), id, req.ConfigText); err != nil {
		writeError(w, http.StatusBadRequest, "SITE_CONFIG_ERROR", err.Error())
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "update_site_conf", strconv.FormatInt(id, 10), "validated", clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "无效的站点 ID")
		return
	}
	if err := s.nginx.DeleteSite(r.Context(), id); err != nil {
		if errors.Is(err, nginx.ErrSiteNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "站点不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "delete_site", strconv.FormatInt(id, 10), "", clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListCerts(w http.ResponseWriter, r *http.Request) {
	items, err := s.nginx.ListCerts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleIssueCert(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	var req nginx.IssueSSLInput
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	cert, err := s.nginx.IssueSSL(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "SSL_ISSUE_FAILED", err.Error())
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "issue_ssl", cert.Domain, cert.Provider, clientIP(r))
	writeJSON(w, http.StatusCreated, cert)
}

func (s *Server) handleDeleteCert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "无效的证书 ID")
		return
	}
	if err := s.nginx.DeleteCert(r.Context(), id); err != nil {
		if errors.Is(err, nginx.ErrCertNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "证书不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Native Databases ---

func (s *Server) handleGetDB(w http.ResponseWriter, r *http.Request) {
	inst, err := s.dbSvc.GetInstance(r.Context(), r.PathValue("engine"))
	if err != nil {
		if errors.Is(err, database.ErrInvalidDB) {
			writeError(w, http.StatusBadRequest, "INVALID_ENGINE", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleInstallDB(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	engine := r.PathValue("engine")
	var req database.InstallInput
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	tsk, err := s.dbSvc.Install(r.Context(), engine, req)
	if err != nil {
		switch {
		case errors.Is(err, database.ErrDBExists):
			writeError(w, http.StatusConflict, "DB_EXISTS", "该数据库已安装")
		case errors.Is(err, database.ErrInvalidDB):
			writeError(w, http.StatusBadRequest, "INVALID_DB", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		}
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "install_db", engine, tsk.ID, clientIP(r))
	writeJSON(w, http.StatusAccepted, tsk)
}

func (s *Server) handleActionDB(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	engine := r.PathValue("engine")
	var req struct {
		Action string `json:"action"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	inst, err := s.dbSvc.Action(r.Context(), engine, req.Action)
	if err != nil {
		writeError(w, http.StatusBadRequest, "DB_ACTION_FAILED", err.Error())
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "db_"+req.Action, engine, inst.Status, clientIP(r))
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleGetDBConfig(w http.ResponseWriter, r *http.Request) {
	cfgText, err := s.dbSvc.ReadConfig(r.Context(), r.PathValue("engine"))
	if err != nil {
		if errors.Is(err, database.ErrDBNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "数据库尚未安装")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"config_text": cfgText})
}

func (s *Server) handleUpdateDBConfig(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	engine := r.PathValue("engine")
	var req struct {
		ConfigText string `json:"config_text"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	if err := s.dbSvc.UpdateConfig(r.Context(), engine, req.ConfigText); err != nil {
		writeError(w, http.StatusBadRequest, "DB_CONFIG_ERROR", err.Error())
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "update_db_conf", engine, "saved", clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListDBSchemas(w http.ResponseWriter, r *http.Request) {
	items, err := s.dbSvc.ListSchemas(r.Context(), r.PathValue("engine"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_DB", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateDBSchema(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	engine := r.PathValue("engine")
	var req database.CreateSchemaInput
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	sc, err := s.dbSvc.CreateSchema(r.Context(), engine, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "CREATE_SCHEMA_FAILED", err.Error())
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "create_db_schema", engine+":"+sc.DBName, sc.DBUser, clientIP(r))
	writeJSON(w, http.StatusCreated, sc)
}

func (s *Server) handleDeleteDBSchema(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "无效的库 ID")
		return
	}
	if err := s.dbSvc.DeleteSchema(r.Context(), r.PathValue("engine"), id); err != nil {
		writeError(w, http.StatusBadRequest, "DELETE_SCHEMA_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListDBBackups(w http.ResponseWriter, r *http.Request) {
	items, err := s.dbSvc.ListBackups(r.Context(), r.PathValue("engine"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_DB", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreateDBBackup(w http.ResponseWriter, r *http.Request) {
	u := userFromContext(r.Context())
	engine := r.PathValue("engine")
	var req struct {
		DBName string `json:"db_name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
		return
	}
	bk, err := s.dbSvc.BackupDatabase(r.Context(), engine, req.DBName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BACKUP_FAILED", err.Error())
		return
	}
	s.auth.RecordAudit(r.Context(), u.Username, "backup_db", engine+":"+bk.DBName, bk.FilePath, clientIP(r))
	writeJSON(w, http.StatusCreated, bk)
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "API 路径不存在")
		return
	}

	clean := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if clean == "" {
		clean = "index.html"
	}

	if f, err := s.webFS.Open(clean); err == nil {
		_ = f.Close()
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}

	data, err := fs.ReadFile(s.webFS, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) requireAuth(enforcePasswordChanged bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		user, err := s.auth.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
			return
		}
		if enforcePasswordChanged && user.MustChangePassword {
			writeError(w, http.StatusForbidden, "PASSWORD_CHANGE_REQUIRED", "首次登录必须修改初始密码后才能继续操作")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("http handler panicked",
					"path", r.URL.Path,
					"panic", fmt.Sprintf("%v", rec),
					"stack", string(debug.Stack()),
				)
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "服务器内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"ip", clientIP(r),
			)
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("http.Hijacker not supported")
	}
	return hj.Hijack()
}

func userFromContext(ctx context.Context) auth.User {
	u, _ := ctx.Value(userContextKey).(auth.User)
	return u
}

func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if c, err := r.Cookie(sessionCookieName); err == nil {
		return strings.TrimSpace(c.Value)
	}
	return ""
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{
		"code":    code,
		"message": msg,
	})
}
