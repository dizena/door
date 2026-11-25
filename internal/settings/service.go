package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"door/internal/config"
)

var (
	ErrInvalidSetting = errors.New("invalid panel setting")
	ErrInvalidMenu    = errors.New("invalid menu configuration")
	ErrMenuNotFound   = errors.New("menu item not found")
)

// PanelSettings holds user-configurable settings for Door itself.
type PanelSettings struct {
	PanelName       string `json:"panel_name"`
	ListenAddr      string `json:"listen_addr"`
	ServerDir       string `json:"server_dir"`
	DatabaseDir     string `json:"database_dir"`
	ProjectDir      string `json:"project_dir"`
	NginxDir        string `json:"nginx_dir"`
	MemoryLimitMB   int64  `json:"memory_limit_mb"`
	SessionTTLHours int    `json:"session_ttl_hours"`
	SafeEntry       string `json:"safe_entry"`
}

// MenuNode represents a Level-1 or Level-2 item in the panel sidebar tree.
type MenuNode struct {
	ID        int64      `json:"id"`
	ParentID  *int64     `json:"parent_id"`
	Key       string     `json:"key"`
	Title     string     `json:"title"`
	Icon      string     `json:"icon"`
	Route     string     `json:"route"`
	SortOrder int        `json:"sort_order"`
	Visible   bool       `json:"visible"`
	IsBuiltin bool       `json:"is_builtin"`
	Children  []MenuNode `json:"children"`
}

// MenuTreeInput represents a desired 2-level menu tree submitted by drag-and-drop UI.
type MenuTreeInput struct {
	ID       int64           `json:"id"`
	Title    string          `json:"title,omitempty"`
	Icon     string          `json:"icon,omitempty"`
	Visible  *bool           `json:"visible,omitempty"`
	Children []MenuTreeInput `json:"children,omitempty"`
}

// MenuUpdateInput updates display attributes of a single menu item.
type MenuUpdateInput struct {
	Title   string `json:"title"`
	Icon    string `json:"icon"`
	Visible *bool  `json:"visible"`
}

type defaultMenuSeed struct {
	Key      string
	Title    string
	Icon     string
	Route    string
	Children []defaultMenuSeed
}

var defaultMenuSeeds = []defaultMenuSeed{
	{
		Key:   "dashboard",
		Title: "仪表盘",
		Icon:  "dashboard",
		Route: "/dashboard",
	},
	{
		Key:   "nginx",
		Title: "Nginx 与网站",
		Icon:  "globe",
		Route: "/nginx",
		Children: []defaultMenuSeed{
			{Key: "nginx-sites", Title: "站点管理", Icon: "layers", Route: "/nginx/sites"},
			{Key: "nginx-ssl", Title: "SSL 证书", Icon: "shield-check", Route: "/nginx/ssl"},
			{Key: "nginx-service", Title: "Nginx 服务", Icon: "server", Route: "/nginx/service"},
		},
	},
	{
		Key:   "projects",
		Title: "项目部署",
		Icon:  "box",
		Route: "/projects",
		Children: []defaultMenuSeed{
			{Key: "projects-go", Title: "Go 项目", Icon: "code", Route: "/projects/go"},
			{Key: "projects-java", Title: "Java 项目", Icon: "coffee", Route: "/projects/java"},
			{Key: "projects-python", Title: "Python 项目", Icon: "terminal", Route: "/projects/python"},
			{Key: "projects-runtimes", Title: "运行环境", Icon: "cpu", Route: "/projects/runtimes"},
		},
	},
	{
		Key:   "databases",
		Title: "数据库",
		Icon:  "database",
		Route: "/databases",
		Children: []defaultMenuSeed{
			{Key: "db-mysql", Title: "MySQL", Icon: "database", Route: "/databases/mysql"},
			{Key: "db-mongodb", Title: "MongoDB", Icon: "database", Route: "/databases/mongodb"},
			{Key: "db-redis", Title: "Redis", Icon: "zap", Route: "/databases/redis"},
			{Key: "db-postgresql", Title: "PostgreSQL", Icon: "database", Route: "/databases/postgresql"},
		},
	},
	{
		Key:   "system",
		Title: "系统监控与终端",
		Icon:  "activity",
		Route: "/system",
		Children: []defaultMenuSeed{
			{Key: "sys-tasks", Title: "任务队列", Icon: "list-checks", Route: "/system/tasks"},
			{Key: "sys-terminal", Title: "Web 终端", Icon: "terminal", Route: "/system/terminal"},
			{Key: "sys-audit", Title: "审计日志", Icon: "file-text", Route: "/system/audit"},
		},
	},
	{
		Key:   "settings",
		Title: "面板设置",
		Icon:  "settings",
		Route: "/settings",
		Children: []defaultMenuSeed{
			{Key: "settings-general", Title: "基础设置", Icon: "sliders", Route: "/settings/general"},
			{Key: "settings-menus", Title: "菜单配置", Icon: "menu", Route: "/settings/menus"},
			{Key: "settings-security", Title: "安全设置", Icon: "lock", Route: "/settings/security"},
		},
	},
}

// Service manages panel settings and the draggable 2-level menu tree.
type Service struct {
	db *sql.DB
}

// NewService creates a settings Service backed by SQLite.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// EnsureDefaults populates initial panel settings and default 2-level menus if empty.
func (s *Service) EnsureDefaults(ctx context.Context, cfg config.Config) error {
	ttlHours := int(cfg.SessionTTL / time.Hour)
	if ttlHours <= 0 {
		ttlHours = 24
	}
	defaults := map[string]string{
		"panel_name":        "Door 运维面板",
		"listen_addr":       cfg.Addr,
		"server_dir":        cfg.Paths.ServerDir,
		"database_dir":      cfg.Paths.DatabaseDir,
		"project_dir":       cfg.Paths.ProjectDir,
		"nginx_dir":         cfg.Paths.NginxDir,
		"memory_limit_mb":   strconv.FormatInt(cfg.MemoryLimitMB, 10),
		"session_ttl_hours": strconv.Itoa(ttlHours),
		"safe_entry":        "",
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings seed tx: %w", err)
	}
	defer tx.Rollback()

	for k, v := range defaults {
		if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)
`, k, v); err != nil {
			return fmt.Errorf("seed setting %s: %w", k, err)
		}
	}

	var menuCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM menus`).Scan(&menuCount); err != nil {
		return fmt.Errorf("count menus: %w", err)
	}
	if menuCount == 0 {
		if err := insertDefaultMenusTx(ctx, tx); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func insertDefaultMenusTx(ctx context.Context, tx *sql.Tx) error {
	for i, parent := range defaultMenuSeeds {
		res, err := tx.ExecContext(ctx, `
INSERT INTO menus (parent_id, menu_key, title, icon, route, sort_order, visible, is_builtin)
VALUES (NULL, ?, ?, ?, ?, ?, 1, 1)
`, parent.Key, parent.Title, parent.Icon, parent.Route, i)
		if err != nil {
			return fmt.Errorf("insert parent menu %s: %w", parent.Key, err)
		}
		parentID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("get parent menu id %s: %w", parent.Key, err)
		}
		for j, child := range parent.Children {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO menus (parent_id, menu_key, title, icon, route, sort_order, visible, is_builtin)
VALUES (?, ?, ?, ?, ?, ?, 1, 1)
`, parentID, child.Key, child.Title, child.Icon, child.Route, j); err != nil {
				return fmt.Errorf("insert child menu %s: %w", child.Key, err)
			}
		}
	}
	return nil
}

// GetPanelSettings returns the persisted panel configuration.
func (s *Service) GetPanelSettings(ctx context.Context) (PanelSettings, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return PanelSettings{}, fmt.Errorf("query settings: %w", err)
	}
	defer rows.Close()

	kv := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return PanelSettings{}, fmt.Errorf("scan setting: %w", err)
		}
		kv[k] = v
	}
	if err := rows.Err(); err != nil {
		return PanelSettings{}, err
	}

	memLimit, _ := strconv.ParseInt(kv["memory_limit_mb"], 10, 64)
	ttlHours, _ := strconv.Atoi(kv["session_ttl_hours"])
	if ttlHours <= 0 {
		ttlHours = 24
	}

	return PanelSettings{
		PanelName:       kv["panel_name"],
		ListenAddr:      kv["listen_addr"],
		ServerDir:       kv["server_dir"],
		DatabaseDir:     kv["database_dir"],
		ProjectDir:      kv["project_dir"],
		NginxDir:        kv["nginx_dir"],
		MemoryLimitMB:   memLimit,
		SessionTTLHours: ttlHours,
		SafeEntry:       kv["safe_entry"],
	}, nil
}

// UpdatePanelSettings validates and persists updated panel settings.
func (s *Service) UpdatePanelSettings(ctx context.Context, in PanelSettings) (PanelSettings, error) {
	in.PanelName = strings.TrimSpace(in.PanelName)
	in.ListenAddr = strings.TrimSpace(in.ListenAddr)
	in.SafeEntry = strings.TrimSpace(in.SafeEntry)

	if in.PanelName == "" || len([]rune(in.PanelName)) > 48 {
		return PanelSettings{}, fmt.Errorf("%w: panel_name must be 1..48 characters", ErrInvalidSetting)
	}
	if err := validateListenAddr(in.ListenAddr); err != nil {
		return PanelSettings{}, err
	}

	dirs := []*string{&in.ServerDir, &in.DatabaseDir, &in.ProjectDir, &in.NginxDir}
	for _, d := range dirs {
		clean, err := validateDirSetting(*d)
		if err != nil {
			return PanelSettings{}, err
		}
		*d = clean
	}

	if in.MemoryLimitMB < 0 || in.MemoryLimitMB > 65536 {
		return PanelSettings{}, fmt.Errorf("%w: memory_limit_mb must be between 0 and 65536", ErrInvalidSetting)
	}
	if in.SessionTTLHours < 1 || in.SessionTTLHours > 720 {
		return PanelSettings{}, fmt.Errorf("%w: session_ttl_hours must be between 1 and 720", ErrInvalidSetting)
	}
	if in.SafeEntry != "" {
		if !strings.HasPrefix(in.SafeEntry, "/") || strings.ContainsAny(in.SafeEntry, " \t\r\n..") {
			return PanelSettings{}, fmt.Errorf("%w: safe_entry must start with / and contain no spaces or '..'", ErrInvalidSetting)
		}
	}

	updates := map[string]string{
		"panel_name":        in.PanelName,
		"listen_addr":       in.ListenAddr,
		"server_dir":        in.ServerDir,
		"database_dir":      in.DatabaseDir,
		"project_dir":       in.ProjectDir,
		"nginx_dir":         in.NginxDir,
		"memory_limit_mb":   strconv.FormatInt(in.MemoryLimitMB, 10),
		"session_ttl_hours": strconv.Itoa(in.SessionTTLHours),
		"safe_entry":        in.SafeEntry,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PanelSettings{}, fmt.Errorf("begin update settings tx: %w", err)
	}
	defer tx.Rollback()

	for k, v := range updates {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO settings (key, value, updated_at)
VALUES (?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
`, k, v); err != nil {
			return PanelSettings{}, fmt.Errorf("upsert setting %s: %w", k, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return PanelSettings{}, fmt.Errorf("commit settings: %w", err)
	}
	return s.GetPanelSettings(ctx)
}

// ListMenuTree returns the complete ordered 2-level menu tree.
func (s *Service) ListMenuTree(ctx context.Context) ([]MenuNode, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, parent_id, menu_key, title, icon, route, sort_order, visible, is_builtin
FROM menus
ORDER BY COALESCE(parent_id, 0) ASC, sort_order ASC, id ASC
`)
	if err != nil {
		return nil, fmt.Errorf("query menus: %w", err)
	}
	defer rows.Close()

	var (
		parents     []MenuNode
		childrenMap = make(map[int64][]MenuNode)
	)

	for rows.Next() {
		var (
			n       MenuNode
			pid     sql.NullInt64
			visible int
			builtin int
		)
		if err := rows.Scan(&n.ID, &pid, &n.Key, &n.Title, &n.Icon, &n.Route, &n.SortOrder, &visible, &builtin); err != nil {
			return nil, fmt.Errorf("scan menu: %w", err)
		}
		n.Visible = visible == 1
		n.IsBuiltin = builtin == 1
		n.Children = []MenuNode{}
		if pid.Valid {
			v := pid.Int64
			n.ParentID = &v
			childrenMap[v] = append(childrenMap[v], n)
		} else {
			parents = append(parents, n)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range parents {
		if kids, ok := childrenMap[parents[i].ID]; ok {
			parents[i].Children = kids
		}
	}
	return parents, nil
}

// UpdateMenuItem updates a single menu node's title, icon, or visibility.
func (s *Service) UpdateMenuItem(ctx context.Context, id int64, in MenuUpdateInput) (MenuNode, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" || len([]rune(title)) > 32 {
		return MenuNode{}, fmt.Errorf("%w: title must be 1..32 characters", ErrInvalidMenu)
	}
	icon := strings.TrimSpace(in.Icon)
	if len(icon) > 32 {
		return MenuNode{}, fmt.Errorf("%w: icon name too long", ErrInvalidMenu)
	}

	var curVisible int
	err := s.db.QueryRowContext(ctx, `SELECT visible FROM menus WHERE id = ?`, id).Scan(&curVisible)
	if errors.Is(err, sql.ErrNoRows) {
		return MenuNode{}, ErrMenuNotFound
	}
	if err != nil {
		return MenuNode{}, fmt.Errorf("lookup menu %d: %w", id, err)
	}

	vis := curVisible
	if in.Visible != nil {
		if *in.Visible {
			vis = 1
		} else {
			vis = 0
		}
	}

	if _, err := s.db.ExecContext(ctx, `
UPDATE menus SET title = ?, icon = ?, visible = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, title, icon, vis, id); err != nil {
		return MenuNode{}, fmt.Errorf("update menu %d: %w", id, err)
	}

	tree, err := s.ListMenuTree(ctx)
	if err != nil {
		return MenuNode{}, err
	}
	for _, p := range tree {
		if p.ID == id {
			return p, nil
		}
		for _, c := range p.Children {
			if c.ID == id {
				return c, nil
			}
		}
	}
	return MenuNode{}, ErrMenuNotFound
}

// ReorderMenuTree atomically saves drag-and-drop ordering and Level-1/Level-2 parentage.
func (s *Service) ReorderMenuTree(ctx context.Context, nodes []MenuTreeInput) ([]MenuNode, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: menu tree cannot be empty", ErrInvalidMenu)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin menu reorder tx: %w", err)
	}
	defer tx.Rollback()

	existing := make(map[int64]struct{})
	rows, err := tx.QueryContext(ctx, `SELECT id FROM menus`)
	if err != nil {
		return nil, fmt.Errorf("load existing menus: %w", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan menu id: %w", err)
		}
		existing[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	seen := make(map[int64]struct{}, len(existing))
	type flatUpdate struct {
		id        int64
		parentID  sql.NullInt64
		sortOrder int
		title     string
		icon      string
		visible   *bool
	}
	var updates []flatUpdate

	registerNode := func(n MenuTreeInput, parentID sql.NullInt64, idx int) error {
		if n.ID <= 0 {
			return fmt.Errorf("%w: invalid menu id %d", ErrInvalidMenu, n.ID)
		}
		if _, ok := existing[n.ID]; !ok {
			return fmt.Errorf("%w: unknown menu id %d", ErrMenuNotFound, n.ID)
		}
		if _, dup := seen[n.ID]; dup {
			return fmt.Errorf("%w: duplicate menu id %d", ErrInvalidMenu, n.ID)
		}
		seen[n.ID] = struct{}{}
		updates = append(updates, flatUpdate{
			id:        n.ID,
			parentID:  parentID,
			sortOrder: idx,
			title:     strings.TrimSpace(n.Title),
			icon:      strings.TrimSpace(n.Icon),
			visible:   n.Visible,
		})
		return nil
	}

	for i, p := range nodes {
		if err := registerNode(p, sql.NullInt64{}, i); err != nil {
			return nil, err
		}
		for j, c := range p.Children {
			if len(c.Children) > 0 {
				return nil, fmt.Errorf("%w: menu depth cannot exceed 2 levels (item %d has nested children)", ErrInvalidMenu, c.ID)
			}
			if err := registerNode(c, sql.NullInt64{Int64: p.ID, Valid: true}, j); err != nil {
				return nil, err
			}
		}
	}

	if len(seen) != len(existing) {
		return nil, fmt.Errorf("%w: submitted tree has %d items, expected %d", ErrInvalidMenu, len(seen), len(existing))
	}

	for _, u := range updates {
		if _, err := tx.ExecContext(ctx, `
UPDATE menus
SET parent_id = ?, sort_order = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`, u.parentID, u.sortOrder, u.id); err != nil {
			return nil, fmt.Errorf("update menu %d hierarchy: %w", u.id, err)
		}

		if u.title != "" {
			if len([]rune(u.title)) > 32 {
				return nil, fmt.Errorf("%w: title too long for menu %d", ErrInvalidMenu, u.id)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE menus SET title = ? WHERE id = ?`, u.title, u.id); err != nil {
				return nil, fmt.Errorf("update menu %d title: %w", u.id, err)
			}
		}
		if u.icon != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE menus SET icon = ? WHERE id = ?`, u.icon, u.id); err != nil {
				return nil, fmt.Errorf("update menu %d icon: %w", u.id, err)
			}
		}
		if u.visible != nil {
			vis := 0
			if *u.visible {
				vis = 1
			}
			if _, err := tx.ExecContext(ctx, `UPDATE menus SET visible = ? WHERE id = ?`, vis, u.id); err != nil {
				return nil, fmt.Errorf("update menu %d visibility: %w", u.id, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit menu reorder: %w", err)
	}
	return s.ListMenuTree(ctx)
}

// ResetDefaultMenus restores the built-in BaoTa-style 2-level menu tree.
func (s *Service) ResetDefaultMenus(ctx context.Context) ([]MenuNode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin reset menus tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM menus`); err != nil {
		return nil, fmt.Errorf("clear menus: %w", err)
	}
	if err := insertDefaultMenusTx(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reset menus: %w", err)
	}
	return s.ListMenuTree(ctx)
}

func validateListenAddr(addr string) error {
	if addr == "" {
		return fmt.Errorf("%w: listen_addr cannot be empty", ErrInvalidSetting)
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%w: invalid listen_addr %q", ErrInvalidSetting, addr)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%w: invalid port in listen_addr %q", ErrInvalidSetting, addr)
	}
	return nil
}

func validateDirSetting(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: directory path cannot be empty", ErrInvalidSetting)
	}
	if strings.Contains(raw, "..") {
		return "", fmt.Errorf("%w: directory path cannot contain '..' (%s)", ErrInvalidSetting, raw)
	}
	clean := filepath.Clean(raw)
	if !filepath.IsAbs(clean) {
		return "", fmt.Errorf("%w: directory path must be absolute (%s)", ErrInvalidSetting, raw)
	}
	return clean, nil
}
