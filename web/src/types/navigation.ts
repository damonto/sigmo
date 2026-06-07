import type { Component } from 'vue'
import type { RouteLocationRaw } from 'vue-router'

export type NavigationItem = {
  key: string
  to: RouteLocationRaw
  routeName: string
  activeRouteNames?: string[]
  label: string
  icon: Component
}
