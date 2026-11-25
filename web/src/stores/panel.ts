import { reactive } from 'vue'
import { api, ApiError, type MenuNode, type User } from '../api/client'

export const panelState = reactive({
  initialized: false,
  user: null as User | null,
  panelName: 'Door 运维面板',
  driverMode: 'mock-local',
  dev: true,
  menus: [] as MenuNode[],
})

export async function loadSession(): Promise<'ok' | 'unauthenticated' | 'force-password'> {
  try {
    const me = await api.me()
    panelState.user = me.user
    panelState.panelName = me.panel_name || 'Door 运维面板'
    panelState.driverMode = me.driver_mode
    panelState.dev = me.dev
    panelState.initialized = true

    if (me.user.must_change_password) {
      panelState.menus = []
      return 'force-password'
    }

    const menuRes = await api.getMenus()
    panelState.menus = menuRes.items || []
    return 'ok'
  } catch (err) {
    panelState.initialized = true
    if (err instanceof ApiError && err.status === 401) {
      panelState.user = null
      panelState.menus = []
      return 'unauthenticated'
    }
    if (err instanceof ApiError && err.code === 'PASSWORD_CHANGE_REQUIRED') {
      return 'force-password'
    }
    throw err
  }
}

export async function refreshMenus() {
  const res = await api.getMenus()
  panelState.menus = res.items || []
}

export async function logoutSession() {
  try {
    await api.logout()
  } finally {
    panelState.user = null
    panelState.menus = []
  }
}
