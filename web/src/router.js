import { createRouter, createWebHistory } from 'vue-router'
import DashboardView from './views/DashboardView.vue'
import PipelineView from './views/PipelineView.vue'
import SettingsView from './views/SettingsView.vue'
import JobDetailView from './views/JobDetailView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'dashboard',
      component: DashboardView
    },
    {
      path: '/pipeline',
      name: 'pipeline',
      component: PipelineView
    },
    {
      path: '/settings',
      name: 'settings',
      component: SettingsView
    },
    {
      path: '/jobs/:id',
      name: 'job-detail',
      component: JobDetailView
    }
  ]
})

export default router
