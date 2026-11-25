package settings

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"door/internal/config"
	"door/internal/store"
)

func TestPanelSettingsAndDraggableMenuTree(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "door.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	cfg := config.Default()
	cfg.Dev = true
	cfg.DevRoot = t.TempDir()
	if err := cfg.Resolve(); err != nil {
		t.Fatalf("resolve cfg: %v", err)
	}

	svc := NewService(db)
	if err := svc.EnsureDefaults(ctx, cfg); err != nil {
		t.Fatalf("EnsureDefaults failed: %v", err)
	}

	ps, err := svc.GetPanelSettings(ctx)
	if err != nil {
		t.Fatalf("GetPanelSettings failed: %v", err)
	}
	if ps.ListenAddr != ":12345" || ps.ServerDir != cfg.Paths.ServerDir {
		t.Fatalf("unexpected defaults: %+v", ps)
	}

	// Update settings validates invalid port and relative dirs.
	bad := ps
	bad.ListenAddr = ":99999"
	if _, err := svc.UpdatePanelSettings(ctx, bad); !errors.Is(err, ErrInvalidSetting) {
		t.Fatalf("expected ErrInvalidSetting for bad port, got %v", err)
	}

	ps.PanelName = "My Door Panel"
	updated, err := svc.UpdatePanelSettings(ctx, ps)
	if err != nil || updated.PanelName != "My Door Panel" {
		t.Fatalf("UpdatePanelSettings failed: %+v err=%v", updated, err)
	}

	// Verify default 2-level menu tree.
	tree, err := svc.ListMenuTree(ctx)
	if err != nil {
		t.Fatalf("ListMenuTree failed: %v", err)
	}
	if len(tree) != 6 {
		t.Fatalf("expected 6 level-1 menus, got %d", len(tree))
	}

	// Find databases menu and redis child, plus dashboard (childless level-1).
	var dbMenu, dashMenu, projectsMenu MenuNode
	for _, n := range tree {
		switch n.Key {
		case "databases":
			dbMenu = n
		case "dashboard":
			dashMenu = n
		case "projects":
			projectsMenu = n
		}
	}
	if len(dbMenu.Children) != 4 {
		t.Fatalf("expected 4 database submenus, got %d", len(dbMenu.Children))
	}

	// Drag-and-drop test:
	// 1) Promote "db-redis" from databases to Level-1.
	// 2) Move "projects-go" from projects into databases.
	// 3) Reject 3-level nesting!
	redisChild := dbMenu.Children[2]
	goChild := projectsMenu.Children[0]

	badDepth := make([]MenuTreeInput, 0, len(tree))
	for _, p := range tree {
		in := MenuTreeInput{ID: p.ID}
		for _, c := range p.Children {
			ci := MenuTreeInput{ID: c.ID}
			if p.Key == "databases" && c.ID == dbMenu.Children[0].ID {
				ci.Children = []MenuTreeInput{{ID: redisChild.ID}}
			} else if c.ID == redisChild.ID {
				continue
			}
			in.Children = append(in.Children, ci)
		}
		badDepth = append(badDepth, in)
	}
	if _, err := svc.ReorderMenuTree(ctx, badDepth); !errors.Is(err, ErrInvalidMenu) {
		t.Fatalf("expected ErrInvalidMenu for 3-level nesting, got %v", err)
	}

	// Valid reorder: swap dashboard and databases, promote redisChild to Level-1, move goChild to databases.
	var valid []MenuTreeInput
	for _, p := range tree {
		switch p.Key {
		case "dashboard":
			continue
		case "databases":
			in := MenuTreeInput{ID: p.ID}
			for _, c := range p.Children {
				if c.ID == redisChild.ID {
					continue
				}
				in.Children = append(in.Children, MenuTreeInput{ID: c.ID})
			}
			in.Children = append(in.Children, MenuTreeInput{ID: goChild.ID})
			valid = append(valid, in)
		case "projects":
			in := MenuTreeInput{ID: p.ID}
			for _, c := range p.Children {
				if c.ID == goChild.ID {
					continue
				}
				in.Children = append(in.Children, MenuTreeInput{ID: c.ID})
			}
			valid = append(valid, in)
		default:
			in := MenuTreeInput{ID: p.ID}
			for _, c := range p.Children {
				in.Children = append(in.Children, MenuTreeInput{ID: c.ID})
			}
			valid = append(valid, in)
		}
	}
	valid = append(valid, MenuTreeInput{ID: dashMenu.ID}, MenuTreeInput{ID: redisChild.ID})

	reordered, err := svc.ReorderMenuTree(ctx, valid)
	if err != nil {
		t.Fatalf("ReorderMenuTree failed: %v", err)
	}
	if reordered[0].Key != "nginx" || reordered[len(reordered)-1].Key != "db-redis" {
		t.Fatalf("unexpected reorder result: first=%s last=%s", reordered[0].Key, reordered[len(reordered)-1].Key)
	}

	// Reset to default restores original 6 groups.
	reset, err := svc.ResetDefaultMenus(ctx)
	if err != nil {
		t.Fatalf("ResetDefaultMenus failed: %v", err)
	}
	if len(reset) != 6 || reset[0].Key != "dashboard" {
		t.Fatalf("unexpected reset tree: %+v", reset)
	}
}
