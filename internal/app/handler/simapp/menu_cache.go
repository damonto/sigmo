package simapp

import (
	"strings"
	"sync"
)

type menuCache struct {
	mu    sync.RWMutex
	menus map[menuCacheKey]*wsMenu
}

type menuCacheKey struct {
	imei  string
	iccid string
}

func newMenuCache() *menuCache {
	return &menuCache{
		menus: make(map[menuCacheKey]*wsMenu),
	}
}

func (c *menuCache) Get(imei, iccid string) *wsMenu {
	key, ok := newMenuCacheKey(imei, iccid)
	if !ok || c == nil {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneMenu(c.menus[key])
}

func (c *menuCache) Set(imei, iccid string, menu *wsMenu) {
	key, ok := newMenuCacheKey(imei, iccid)
	if !ok || c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if menu == nil {
		delete(c.menus, key)
		return
	}
	if c.menus == nil {
		c.menus = make(map[menuCacheKey]*wsMenu)
	}
	c.menus[key] = cloneMenu(menu)
}

func newMenuCacheKey(imei, iccid string) (menuCacheKey, bool) {
	key := menuCacheKey{
		imei:  strings.TrimSpace(imei),
		iccid: strings.TrimSpace(iccid),
	}
	return key, key.imei != "" && key.iccid != ""
}
