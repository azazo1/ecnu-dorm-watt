import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'dashboard',
      component: () => import('@/pages/DashboardView.vue'),
      meta: { title: '仪表盘' },
    },
    {
      path: '/records',
      name: 'records',
      component: () => import('@/pages/RecordsView.vue'),
      meta: { title: '记录' },
    },
    {
      path: '/archives',
      name: 'archives',
      component: () => import('@/pages/ArchivesView.vue'),
      meta: { title: '归档' },
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('@/pages/SettingsView.vue'),
      meta: { title: '设置' },
    },
    {
      path: '/login-help',
      name: 'login-help',
      component: () => import('@/pages/LoginHelpView.vue'),
      meta: { title: '登录帮助' },
    },
    {
      path: '/:pathMatch(.*)*',
      redirect: '/',
    },
  ],
  scrollBehavior(to) {
    if (to.hash) {
      return { el: to.hash, behavior: 'smooth', top: 72 }
    }
    return { top: 0 }
  },
})

router.afterEach((to) => {
  const title = typeof to.meta.title === 'string' ? to.meta.title : ''
  document.title = title ? `${title} - DormWatt` : 'DormWatt'
})

export default router
