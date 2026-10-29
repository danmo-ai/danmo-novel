import { createRouter, createWebHistory } from 'vue-router'
import ShelfView from '@/views/ShelfView.vue'
import BookView from '@/views/BookView.vue'
import SettingsView from '@/views/SettingsView.vue'
import RunsView from '@/views/RunsView.vue'
import KnowledgeView from '@/views/KnowledgeView.vue'

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'shelf', component: ShelfView },
    { path: '/books/:id', name: 'book', component: BookView },
    { path: '/runs', name: 'runs', component: RunsView },
    { path: '/knowledge', name: 'knowledge', component: KnowledgeView },
    { path: '/settings', name: 'settings', component: SettingsView },
  ],
})
