<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useInjectedWorkspace } from '../workspace'

/**
 * 单元列表：手机上的「书的首页」。
 *
 * 桌面用左侧单元树 + 顶部五个 Tab 同时给出口；手机一次只做一件事，所以这里
 * 只回答「写哪一个单元」，点进去再决定是细纲、正文还是审校。
 * 卷分组仍然保留（多卷的书需要它），但默认全部展开——手机上折叠一次就
 * 等于让作者多点一次才能看到自己的单元。
 */
// 单元状态文案只有一份（useBookWorkspace）：三个壳说同一句话，才不会出现
// 「草稿」在列表里叫草稿、在详情里叫待审校这种漂移。
const { store, volumeGroups, busy, primaryInfo, statusText } = useInjectedWorkspace()
const router = useRouter()

function open(unitId: string) {
  router.push(`/books/${store.currentBook?.summary.id}/units/${unitId}`)
}
</script>

<template>
  <section v-if="!store.currentBook" class="state panel">加载中…</section>

  <template v-else>
    <article class="next panel">
      <div>
        <small>下一步 · {{ primaryInfo.target }}</small>
        <p>{{ primaryInfo.description }}</p>
      </div>
      <span v-if="busy" class="muted">运行中…</span>
    </article>

    <div v-if="!store.currentBook.units.length" class="state panel">
      <h2>还没有单元</h2>
      <p>先去「设定」确认立项结果，或回立项页生成本书的卷纲；单元会跟着卷纲一起播种。</p>
      <button type="button" @click="router.push(`/books/${store.currentBook.summary.id}/setup`)">去立项</button>
    </div>

    <section v-for="g in volumeGroups" :key="g.volume" class="volume">
      <header class="volume-head">
        <strong>第 {{ g.volume }} 卷</strong>
        <span class="muted">{{ g.reviewed }}/{{ g.units.length }} 已定稿</span>
      </header>
      <p v-if="g.title" class="volume-title">{{ g.title }}</p>
      <ul class="unit-list">
        <li v-for="u in g.units" :key="u.unitId">
          <button class="unit-row panel" type="button" @click="open(u.unitId)">
            <span class="unit-main">
              <span class="unit-line">
                <strong>{{ u.unitId }}</strong>
                <span class="status" :data-status="u.status">{{ statusText[u.status] || u.status }}</span>
              </span>
              <span class="unit-title">{{ u.title || '未完成单元合同' }}</span>
              <small v-if="u.chapterFrom">第 {{ u.chapterFrom }}–{{ u.chapterTo }} 章</small>
            </span>
            <span class="chev" aria-hidden="true">›</span>
          </button>
        </li>
      </ul>
    </section>
  </template>
</template>

<style scoped>
.state { padding: 28px 20px; text-align: center; color: var(--ink-soft); font-size: 13px; line-height: 1.7; }
.state h2 { margin: 0 0 6px; font-family: var(--font-prose); color: var(--ink); }
.state p { margin: 0 0 14px; }
.next {
  display: flex; align-items: center; gap: 12px; justify-content: space-between;
  padding: 12px 14px; margin-bottom: 14px;
  border-color: rgba(15,107,92,.2);
  background: linear-gradient(120deg, rgba(15,107,92,.06), rgba(255,255,255,.75));
}
.next small { color: var(--accent); font-size: 11px; font-weight: 700; }
.next p { margin: 3px 0 0; color: var(--ink-soft); font-size: 12px; line-height: 1.6; }
.muted { color: var(--ink-soft); font-size: 12px; }
.volume { margin-bottom: 18px; }
.volume-head { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; padding: 0 2px 6px; }
.volume-head strong { font-size: 14px; }
.volume-title { margin: 0 2px 8px; color: var(--ink-soft); font-size: 11px; line-height: 1.5; }
.unit-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 8px; }
.unit-row { display: flex; align-items: center; gap: 10px; width: 100%; padding: 12px 14px; text-align: left; }
.unit-main { display: grid; gap: 4px; min-width: 0; flex: 1; }
.unit-line { display: flex; align-items: center; gap: 8px; }
.unit-line strong { font-size: 13px; }
.unit-title { font-size: 13px; color: var(--ink); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.unit-main small { color: var(--ink-soft); font-size: 11px; }
.status { font-size: 10px; padding: 1px 7px; border-radius: 999px; background: rgba(20,35,31,.06); color: var(--ink-soft); }
.status[data-status="reviewed"], .status[data-status="finalized"] { background: rgba(31,122,77,.12); color: var(--ok); }
.status[data-status="drafted"] { background: rgba(161,92,18,.12); color: var(--warn); }
.status[data-status="accepted"] { background: rgba(15,107,92,.12); color: var(--accent); }
.chev { flex-shrink: 0; color: var(--ink-soft); font-size: 20px; line-height: 1; }
</style>
