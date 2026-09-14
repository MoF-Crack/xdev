// app_favorites.go implements the favorites system for the GUI.
//
// Favorites allow users to pin frequently-used tools for quick access
// in the sidebar. Favorites are stored in the config directory as
// favorites.json and persist across sessions.
//
// Provides:
//   - GetFavorites: list all favorited tools
//   - AddFavorite: add a tool to favorites
//   - RemoveFavorite: remove a tool from favorites
package gui

import (
	"fmt"
	"time"
)

// FavoriteInfo 收藏的工具信息
type FavoriteInfo struct {
	Name          string   `json:"name"`
	DisplayName   string   `json:"displayName"`
	Category      string   `json:"category"`
	AddedAt       string   `json:"addedAt"`
	ActiveVersion string   `json:"activeVersion"`        // 当前活跃版本
	Installed     bool     `json:"installed"`            // 是否已通过 xdev 安装
	LocalVersion  string   `json:"localVersion"`         // 系统本地检测到的版本
	LocalPath     string   `json:"localPath"`            // 系统本地路径
	Source        string   `json:"source"`               // "xdev" 或 "local"
	FormFactor    []string `json:"formFactor,omitempty"` // 形态（manifest EffectiveFormFactor，gui/cli/tui）
}

// GetFavorites 返回收藏列表（含版本管理信息）
func (a *App) GetFavorites() []FavoriteInfo {
	a.favMu.RLock()
	base := make([]FavoriteInfo, len(a.favorites))
	copy(base, a.favorites)
	a.favMu.RUnlock()

	// 填充版本管理信息
	for i := range base {
		name := base[i].Name
		// 检查 xdev 管理的安装
		activeVer, hasActive := a.currentVersion(name)
		if hasActive {
			base[i].ActiveVersion = activeVer
			base[i].Installed = true
			base[i].Source = "xdev"
		}
		// 检查本地缓存中的信息
		ver, path, source := a.getLocalCached(name)
		if ver != "" {
			if !hasActive {
				base[i].LocalVersion = ver
				base[i].Source = source
			}
			if path != "" {
				base[i].LocalPath = path
			}
		}
		// 如果 xdev 已安装且本地也有，source 标记为 xdev
		if hasActive && source == "local" {
			base[i].LocalVersion = ver
			base[i].Source = "xdev"
		}
		// 形态信息来自 manifest（Spec 023）。manifest 可能为 nil
		// （Lua 插件/测试桩）——nil 安全，留空即可（omitempty）。
		if p, ok := a.registry.Get(name); ok {
			if m := p.Manifest(); m != nil {
				base[i].FormFactor = m.EffectiveFormFactor()
			}
		}
	}
	return base
}

// AddFavorite 添加工具到收藏
func (a *App) AddFavorite(toolName string) error {
	a.favMu.Lock()
	defer a.favMu.Unlock()
	// 检查是否已收藏
	for _, f := range a.favorites {
		if f.Name == toolName {
			return nil // 已收藏，不重复添加
		}
	}
	// 从 registry 获取工具信息
	p, ok := a.registry.Get(toolName)
	if !ok {
		return fmt.Errorf("tool %q not found", toolName)
	}
	a.favorites = append(a.favorites, FavoriteInfo{
		Name:        toolName,
		DisplayName: p.DisplayName(),
		Category:    p.Category(),
		AddedAt:     time.Now().Format("2006-01-02"),
	})
	if m := p.Manifest(); m != nil {
		a.favorites[len(a.favorites)-1].FormFactor = m.EffectiveFormFactor()
	}
	a.saveFavorites()
	return nil
}

// RemoveFavorite 从收藏中移除
func (a *App) RemoveFavorite(toolName string) error {
	a.favMu.Lock()
	defer a.favMu.Unlock()
	for i, f := range a.favorites {
		if f.Name == toolName {
			a.favorites = append(a.favorites[:i], a.favorites[i+1:]...)
			a.saveFavorites()
			return nil
		}
	}
	return nil
}

// IsFavorite 检查工具是否已收藏
func (a *App) IsFavorite(toolName string) bool {
	a.favMu.RLock()
	defer a.favMu.RUnlock()
	for _, f := range a.favorites {
		if f.Name == toolName {
			return true
		}
	}
	return false
}

// saveFavorites 保存收藏列表到文件
// Caller must hold a.favMu lock.
func (a *App) saveFavorites() {
	if err := a.saveJSONToCache("favorites.json", a.favorites); err != nil {
		logWarn("[saveFavorites] marshal error: %v", err)
	}
}

// loadFavorites 从文件加载收藏列表
func (a *App) loadFavorites() {
	var favs []FavoriteInfo
	if err := a.loadJSONFromCache("favorites.json", &favs); err != nil {
		return
	}
	// Ticket 38 (P2-04): publish the slice under favMu. Wails runs every
	// bound method in its own goroutine, so this assignment raced the
	// readers (GetFavorites / toggleFavorite) that take favMu.RLock.
	a.favMu.Lock()
	a.favorites = favs
	a.favMu.Unlock()
}
