import { createRouter, createWebHistory } from 'vue-router'
import { loadSession, panelState } from '../stores/panel'
import DashboardView from '../views/DashboardView.vue'
import DatabaseView from '../views/DatabaseView.vue'
import ForcePasswordView from '../views/ForcePasswordView.vue'
import LoginView from '../views/LoginView.vue'
import ModuleView from '../views/ModuleView.vue'
import NginxView from '../views/NginxView.vue'
import ProjectsView from '../views/ProjectsView.vue'
import SettingsGeneralView from '../views/SettingsGeneralView.vue'
import SettingsMenusView from '../views/SettingsMenusView.vue'
import SettingsSecurityView from '../views/SettingsSecurityView.vue'
import TerminalView from '../views/TerminalView.vue'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/dashboard' },
    { path: '/login', component: LoginView, meta: { public: true } },
    { path: '/force-password', component: ForcePasswordView, meta: { allowForcedPw: true } },
    { path: '/dashboard', component: DashboardView },
    { path: '/nginx/:section?', component: NginxView },
    { path: '/projects/:section?', component: ProjectsView },
    { path: '/databases/:engine?', component: DatabaseView },
    { path: '/system/terminal', component: TerminalView },
    { path: '/settings/general', component: SettingsGeneralView },
    { path: '/settings/menus', component: SettingsMenusView },
    { path: '/settings/security', component: SettingsSecurityView },
    { path: '/:pathMatch(.*)*', component: ModuleView },
  ],
})

router.beforeEach(async (to) => {
  if (!panelState.initialized) {
    const status = await loadSession()
    if (status === 'unauthenticated' && !to.meta.public) {
      return '/login'
    }
    if (status === 'force-password' && !to.meta.allowForcedPw) {
      return '/force-password'
    }
  }

  if (!panelState.user && !to.meta.public) {
    return '/login'
  }
  if (panelState.user?.must_change_password && !to.meta.allowForcedPw) {
    return '/force-password'
  }
  if (panelState.user && !panelState.user.must_change_password && (to.path === '/login' || to.path === '/force-password')) {
    return '/dashboard'
  }
  return true
})
