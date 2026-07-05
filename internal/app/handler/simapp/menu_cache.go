package simapp

import (
	"strings"
	"sync"
)

type menuCache struct {
	mu    sync.RWMutex
	menus map[menuCacheKey]menuSnapshot
}

type menuCacheKey struct {
	imei  string
	iccid string
}

type menuSource uint8

const (
	menuSourceUnknown menuSource = iota
	menuSourceSetup
	menuSourceProbe
)

type menuSnapshot struct {
	menu         *wsMenu
	source       menuSource
	probeItem    byte
	hasProbeItem bool
}

func newMenuCache() *menuCache {
	return &menuCache{
		menus: make(map[menuCacheKey]menuSnapshot),
	}
}

func (c *menuCache) Get(imei, iccid string) menuSnapshot {
	key, ok := newMenuCacheKey(imei, iccid)
	if !ok || c == nil {
		return menuSnapshot{}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.menus[key].clone()
}

func (c *menuCache) Set(imei, iccid string, snapshot menuSnapshot) {
	key, ok := newMenuCacheKey(imei, iccid)
	if !ok || c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if snapshot.menu == nil {
		delete(c.menus, key)
		return
	}
	if c.menus == nil {
		c.menus = make(map[menuCacheKey]menuSnapshot)
	}
	c.menus[key] = snapshot.clone()
}

func newMenuSnapshot(menu *wsMenu, source menuSource, probeItem byte) menuSnapshot {
	return menuSnapshot{
		menu:         cloneMenu(menu),
		source:       source,
		probeItem:    probeItem,
		hasProbeItem: source == menuSourceProbe,
	}
}

func (s menuSnapshot) clone() menuSnapshot {
	return menuSnapshot{
		menu:         cloneMenu(s.menu),
		source:       s.source,
		probeItem:    s.probeItem,
		hasProbeItem: s.hasProbeItem,
	}
}

func newMenuCacheKey(imei, iccid string) (menuCacheKey, bool) {
	key := menuCacheKey{
		imei:  strings.TrimSpace(imei),
		iccid: strings.TrimSpace(iccid),
	}
	return key, key.imei != "" && key.iccid != ""
}
