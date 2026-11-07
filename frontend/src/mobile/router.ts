import { createRouter, createWebHistory } from 'vue-router'
import ShelfMobile from './views/ShelfMobile.vue'
import BookShell from './views/BookShell.vue'
import UnitListMobile from './views/UnitListMobile.vue'
import UnitDetailMobile from './views/UnitDetailMobile.vue'
import BibleMobile from './views/BibleMobile.vue'
import SetupMobile from './views/SetupMobile.vue'
import RunsView from '@/views/RunsView.vue'
import SettingsMobile from './views/SettingsMobile.vue'

/**
 * 移动端路由 = 移动端的信息架构。
 *
 * 桌面把 立项/写作/大纲/设定/审校 五个面板平铺成一行 Tab；手机上塞不下，
 * 所以改成「书架 → 一本书 → 一个单元」的层级：
 *   - 书内的一级入口只有「单元列表 / 设定 / 立项」；
 *   - 细纲、正文、审校是同一个单元的不同阶段，收在单元详情里用分段切换。
 * 层级本身就是导航，因此不需要任何 @media 断点。
 */
export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'shelf', component: ShelfMobile },
    {
      path: '/books/:id',
      component: BookShell,
      children: [
        { path: '', name: 'book', component: UnitListMobile },
        { path: 'units/:unitId', name: 'unit', component: UnitDetailMobile, props: true },
        { path: 'bible', name: 'bible', component: BibleMobile },
        { path: 'setup', name: 'setup', component: SetupMobile },
      ],
    },
    { path: '/runs', name: 'runs', component: RunsView },
    { path: '/settings', name: 'settings', component: SettingsMobile },
  ],
})
