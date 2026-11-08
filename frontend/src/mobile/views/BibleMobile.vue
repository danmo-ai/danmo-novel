<script setup lang="ts">
import { computed, ref } from 'vue'
import { useBible } from '@/shared/composables/useBible'
import { isProtagonist, parseCastRelations } from '@/shared/utils/castRelations'
import { useSizeClass } from '../useSizeClass'
import { useInjectedWorkspace } from '../workspace'

/**
 * 手机设定页：目录 → 详情两级。
 *
 * 桌面是「左侧目录 + 右侧详情」常驻；手机一屏只放得下一样，所以先给目录，
 * 点进去再显示内容。内容渲染复用 useBible 的解析结果，和桌面是同一份数据。
 */
const ws = useInjectedWorkspace()

const bible = useBible({
  onSave: (path, content) => { void ws.saveFile(path, content) },
  onRewrite: (spec) => ws.startRewrite(spec),
})

const {
  book, docs, summaries, commits, missingCount, groups,
  selectedPath, selectedCast, currentDoc, currentRaw, currentLabel, currentFilePath, currentCast, castTimeline,
  editing, draft, showSecrets, activeGroup, specialView,
  identityEditing, identitySaving, promoting,
  renderMode, mdBlocks, stateView, lockedView,
  canOptimize, optimizeLabel, optimizeHint,
  roleLabel, chooseDoc, chooseCast, chooseGroup, chooseSpecial,
  gateIcon, startEdit, save, scaffoldMissing,
  startIdentityEdit, saveIdentity, closeIdentity, promoteCast,
  requestRewrite, requestIdentityRewrite,
} = bible

const { isRegular } = useSizeClass()

const mode = ref<'list' | 'detail'>('list')
/** 常规宽度下两栏同时存在，目录不再让位给详情；紧凑宽度才需要 mode 切换。 */
const showList = computed(() => isRegular.value || mode.value === 'list')
const showDetail = computed(() => isRegular.value || mode.value === 'detail')

const groupDocs = computed(() => docs.value.filter((d) => d.group === activeGroup.value))

/**
 * 关系在手机上不画图：700×520 的力导向图缩到 390px 后节点文字只剩几个像素。
 * 这里给一张「每人几条关系」的列表，点名字进人物卡看具体关系——数据同源
 * （parseCastRelations），只是换了一种能在小屏读的呈现。
 */
const relations = computed(() => parseCastRelations(book.value?.cast || []))
function relationCount(stem: string) {
  return relations.value.filter((r) => r.from === stem || r.to === stem).length
}

function openDoc(path: string) {
  chooseDoc(path)
  mode.value = 'detail'
}
function openCast(stem: string) {
  chooseCast(stem)
  mode.value = 'detail'
}
function openRelations() {
  chooseSpecial('relations')
  mode.value = 'detail'
}
function backToList() {
  // 只在紧凑宽度下「返回目录」才有意义；常规宽度目录和详情一直在同一屏。
  if (isRegular.value) return
  mode.value = 'list'
  specialView.value = null
}

function saveAndBack() {
  save()
  // 常规宽度保存后仍停留在当前文档，别把作者踢回目录。
  if (!isRegular.value) mode.value = 'list'
}
</script>

<template>
  <div class="bible-split" :class="{ 'is-split': isRegular }">
  <!-- 目录：常规宽度常驻左栏；紧凑宽度只有 list 态才出现。 -->
  <template v-if="showList">
    <div class="toolbar">
      <button v-if="missingCount" type="button" @click="scaffoldMissing">补齐 {{ missingCount }} 项模板</button>
      <span v-if="book" class="muted">{{ docs.filter(d => d.exists).length }}/{{ docs.length }} 已建立 · {{ book.cast?.length || 0 }} 人物</span>
    </div>

    <div class="group-tabs" role="tablist" aria-label="设定分类">
      <button
        v-for="g in groups"
        :key="g"
        type="button"
        role="tab"
        :aria-selected="activeGroup === g"
        :class="{ active: activeGroup === g }"
        @click="chooseGroup(g)"
      >{{ g }}</button>
    </div>

    <ul v-if="activeGroup === '人物与关系'" class="doc-list">
      <li>
        <button class="doc-row panel" type="button" @click="openRelations">
          <span class="doc-main"><strong>人物关系图</strong><small>正式关系表与文字关系</small></span>
          <span class="chev">›</span>
        </button>
      </li>
      <li v-for="c in book?.cast || []" :key="c.stem">
        <button class="doc-row panel" type="button" @click="openCast(c.stem)">
          <span class="doc-main">
            <strong>{{ c.name }}<i class="role">{{ roleLabel(c.role) }}</i><i v-if="c.status === 'candidate'" class="cand">候选</i></strong>
            <small>{{ c.tagline || c.stem }}</small>
          </span>
          <span class="chev">›</span>
        </button>
      </li>
      <li v-if="!(book?.cast || []).length" class="muted empty">批准立项后人物卡进入这里。</li>
    </ul>

    <ul v-else class="doc-list">
      <li v-for="d in groupDocs" :key="d.path">
        <button class="doc-row panel" type="button" @click="openDoc(d.path)">
          <span class="doc-main">
            <strong>{{ d.label }}<i v-if="!d.exists" class="missing">未创建</i></strong>
            <small>{{ d.description }}</small>
          </span>
          <span class="chev">›</span>
        </button>
      </li>
      <template v-if="activeGroup === '连续性账本'">
        <li v-for="p in summaries" :key="p">
          <button class="doc-row panel" type="button" @click="openDoc(p)">
            <span class="doc-main"><strong>卷摘要 · {{ p.split('/').pop()?.replace('.md', '') }}</strong><small>{{ p }}</small></span>
            <span class="chev">›</span>
          </button>
        </li>
        <li v-for="p in commits" :key="p">
          <button class="doc-row panel" type="button" @click="openDoc(p)">
            <span class="doc-main"><strong>定稿账本 · {{ p.split('/').pop()?.replace('.md', '') }}</strong><small>{{ p }}</small></span>
            <span class="chev">›</span>
          </button>
        </li>
      </template>
    </ul>
  </template>

  <!-- 详情：常规宽度常驻右栏；紧凑宽度只有 detail 态才出现。 -->
  <template v-if="showDetail">
    <div class="detail-head">
      <button v-if="!isRegular" type="button" class="back" aria-label="返回目录" @click="backToList">‹</button>
      <div class="detail-title">
        <h2>{{ currentLabel }}</h2>
        <small>{{ currentFilePath }}</small>
      </div>
    </div>

    <div v-if="specialView === 'relations'" class="panel pad">
      <p class="muted relation-note">
        实线关系是人物卡里的「## 关系」表，虚线是「关系锚点 / 与主角关系」等文字关系。
        点名字进人物卡看具体条目。
      </p>
      <ul class="relation-list">
        <li v-for="c in book?.cast || []" :key="c.stem">
          <button type="button" @click="openCast(c.stem)">
            <span class="rel-name">
              {{ c.name }}
              <i v-if="isProtagonist(c.role)" class="rel-badge">主角</i>
            </span>
            <small>{{ relationCount(c.stem) }} 条关系</small>
          </button>
        </li>
      </ul>
      <p v-if="!(book?.cast || []).length" class="muted">还没有人物卡。</p>
    </div>

    <template v-else>
      <div class="actions">
        <button v-if="canOptimize" type="button" :disabled="book?.summary.readOnly" :title="optimizeHint" @click="requestRewrite">{{ optimizeLabel }}</button>
        <button v-if="currentCast?.status === 'candidate'" type="button" :disabled="book?.summary.readOnly || promoting" @click="promoteCast">{{ promoting ? '晋升中…' : '晋升 canon' }}</button>
        <button type="button" class="primary" :disabled="editing ? false : false" @click="editing ? saveAndBack() : startEdit()">
          {{ editing ? '保存更改' : '编辑原始文件' }}
        </button>
        <button v-if="editing" type="button" @click="editing = false">取消</button>
      </div>

      <!-- 人物身份时间线 -->
      <section v-if="selectedCast" class="panel pad identity">
        <header>
          <strong>职务身份时间线</strong>
          <button type="button" :disabled="book?.summary.readOnly" @click="startIdentityEdit()">{{ castTimeline.length ? '新增身份段' : '登记初始身份' }}</button>
        </header>
        <p v-if="!castTimeline.length" class="warn">尚未建立职务身份。正式写作前必须登记初始职务与机构。</p>
        <div v-for="(s, i) in castTimeline" :key="i" class="seg" :class="{ current: !s.toUnit }">
          <span class="range">{{ s.fromUnit }}<template v-if="s.toUnit"> → {{ s.toUnit }}</template><template v-else> → 至今</template></span>
          <strong>{{ s.roleTitle }}</strong>
          <small v-if="s.org">{{ s.org }}</small>
          <small v-if="s.note" class="note">{{ s.note }}</small>
          <div class="seg-actions">
            <button type="button" :disabled="book?.summary.readOnly" @click="startIdentityEdit(i)">修正</button>
            <button v-if="!s.toUnit" type="button" :disabled="book?.summary.readOnly" @click="closeIdentity(i)">结束当前段</button>
          </div>
        </div>
      </section>

      <div v-if="currentDoc?.secret && !showSecrets && !editing" class="panel pad secret">
        <strong>作者底牌</strong>
        <p>这些内容不会进入正文或审校上下文。</p>
        <button type="button" @click="showSecrets = true">显示作者底牌</button>
      </div>

      <textarea v-else-if="editing" v-model="draft" class="source-editor" spellcheck="false" />

      <!-- 书籍运行状态 -->
      <div v-else-if="renderMode === 'state'" class="panel pad">
        <div class="state-grid">
          <div v-for="f in stateView.fields" :key="f.key" class="state-card">
            <small>{{ f.label }}</small><strong>{{ f.value }}</strong>
          </div>
        </div>
        <div v-if="stateView.gates.length" class="gates">
          <span v-for="g in stateView.gates" :key="g.key" class="gate" :data-state="g.value">{{ g.key }} · {{ gateIcon(g.value) }}</span>
        </div>
        <p v-if="!stateView.fields.length" class="muted">尚未写入状态字段。</p>
      </div>

      <!-- 锁词 -->
      <div v-else-if="renderMode === 'locked'" class="panel pad">
        <h3>按卷锁词</h3>
        <table v-if="lockedView.locked.length" class="table">
          <thead><tr><th>锁词</th><th>解锁卷</th></tr></thead>
          <tbody><tr v-for="t in lockedView.locked" :key="t.term"><td>{{ t.term }}</td><td>{{ t.until }}</td></tr></tbody>
        </table>
        <p v-else class="muted">暂无锁词。</p>
        <h3>永久合规红线</h3>
        <ul v-if="lockedView.compliance.length" class="list"><li v-for="c in lockedView.compliance" :key="c">{{ c }}</li></ul>
        <p v-else class="muted">暂无红线。</p>
        <template v-if="lockedView.aliases.length">
          <h3>别名映射</h3>
          <table class="table">
            <thead><tr><th>公开称呼</th><th>真实指向</th></tr></thead>
            <tbody><tr v-for="a in lockedView.aliases" :key="a.alias"><td>{{ a.alias }}</td><td>{{ a.real }}</td></tr></tbody>
          </table>
        </template>
      </div>

      <!-- Markdown 实体 -->
      <div v-else class="panel pad">
        <template v-for="(b, i) in mdBlocks" :key="i">
          <h3 v-if="b.kind === 'heading'" class="md-h" :class="`h${b.level}`">{{ b.text }}</h3>
          <p v-else-if="b.kind === 'para'" class="md-p">{{ b.text }}</p>
          <blockquote v-else-if="b.kind === 'quote'" class="md-q">{{ b.text }}</blockquote>
          <ul v-else-if="b.kind === 'bullets'" class="list">
            <li v-for="(item, j) in b.items" :key="j">{{ item }}</li>
          </ul>
          <div v-else-if="b.kind === 'table'" class="table-wrap">
            <table class="table">
              <thead><tr><th v-for="h in b.headers" :key="h">{{ h || '—' }}</th></tr></thead>
              <tbody>
                <tr v-for="(row, j) in b.rows" :key="j"><td v-for="(cell, k) in row" :key="k">{{ cell || '—' }}</td></tr>
              </tbody>
            </table>
          </div>
        </template>
        <p v-if="!mdBlocks.length" class="muted">文件为空。</p>
      </div>
    </template>
  </template>
  </div>

  <!-- 身份段编辑 -->
  <div v-if="identityEditing" class="sheet-mask" @click.self="identityEditing = null">
    <form class="sheet panel" @submit.prevent="saveIdentity">
      <header>
        <h2>{{ identityEditing.action === 'append' ? '规划身份变更' : identityEditing.action === 'update' ? '修正身份段' : '结束当前身份段' }}</h2>
        <button type="button" class="close" @click="identityEditing = null">×</button>
      </header>
      <p class="muted">身份只从指定单元开始生效；不会改写人物稳定人格。</p>
      <label>职务 / 公开身份
        <input v-model="identityEditing.segment.roleTitle" required :disabled="identityEditing.action === 'close'" placeholder="例如：重案队试用" />
      </label>
      <label>所属机构
        <input v-model="identityEditing.segment.org" required :disabled="identityEditing.action === 'close'" placeholder="例如：青江分局" />
      </label>
      <label>起始单元
        <select v-model="identityEditing.segment.fromUnit" :disabled="identityEditing.action !== 'append'">
          <option v-for="u in book?.units" :key="u.unitId" :value="u.unitId">{{ u.unitId }} · {{ u.title }}</option>
        </select>
      </label>
      <label v-if="identityEditing.action === 'close'">结束于单元
        <select v-model="identityEditing.segment.toUnit" required>
          <option v-for="u in book?.units" :key="u.unitId" :value="u.unitId">{{ u.unitId }} · {{ u.title }}</option>
        </select>
      </label>
      <label v-else>变更原因 / 依据
        <input v-model="identityEditing.segment.note" placeholder="例如：调令生效" />
      </label>
      <div class="sheet-actions">
        <button type="button" @click="identityEditing = null">取消</button>
        <button class="primary" type="submit" :disabled="identitySaving">{{ identitySaving ? '校验中…' : '校验并保存' }}</button>
      </div>
    </form>
  </div>
</template>

<style scoped>
.bible-split { display: grid; gap: 12px; }
.bible-split.is-split {
  grid-template-columns: minmax(220px, 280px) minmax(0, 1fr);
  gap: 16px; align-items: start;
}
.bible-split.is-split .toolbar { margin-bottom: 10px; }
.bible-split.is-split .doc-list { max-height: calc(var(--vvh, 100dvh) - 240px); overflow-y: auto; overscroll-behavior: contain; -webkit-overflow-scrolling: touch; }
.bible-split.is-split .detail-head { margin-bottom: 10px; }
.bible-split.is-split .detail-title h2 { font-size: 22px; }
.toolbar { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 10px; }
.muted { color: var(--ink-soft); font-size: 12px; }
.group-tabs { display: flex; gap: 4px; overflow-x: auto; margin-bottom: 12px; scrollbar-width: none; }
.group-tabs::-webkit-scrollbar { display: none; }
.group-tabs button { flex: 0 0 auto; border: 0; border-bottom: 2px solid transparent; border-radius: 0; background: transparent; color: var(--ink-soft); padding: 8px 10px; white-space: nowrap; }
.group-tabs button.active { color: var(--accent); border-bottom-color: var(--accent); font-weight: 700; }
.doc-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 8px; }
.empty { padding: 16px 4px; text-align: center; }
.doc-row { display: flex; align-items: center; gap: 10px; width: 100%; padding: 12px 14px; text-align: left; }
.doc-main { display: grid; gap: 3px; min-width: 0; flex: 1; }
.doc-main strong { font-size: 14px; display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.doc-main small { color: var(--ink-soft); font-size: 11px; line-height: 1.5; }
.role { font-style: normal; font-size: 9px; padding: 1px 6px; border-radius: 6px; background: rgba(20,35,31,.06); color: var(--ink-soft); }
.role[data-role="protagonist"] { background: rgba(15,107,92,.12); color: var(--accent); }
.cand { font-style: normal; font-size: 9px; padding: 0 5px; border-radius: 6px; background: rgba(161,92,18,.14); color: var(--warn); }
.missing { font-style: normal; font-size: 10px; color: var(--warn); }
.chev { color: var(--ink-soft); font-size: 20px; line-height: 1; flex-shrink: 0; }
.relation-note { margin: 0 0 10px; line-height: 1.7; }
.relation-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 8px; }
.relation-list button {
  display: flex; align-items: center; justify-content: space-between; gap: 10px;
  width: 100%; padding: 12px 14px; text-align: left; font-size: 14px;
}
.rel-name { display: inline-flex; align-items: center; gap: 6px; font-weight: 600; }
.rel-badge {
  font-style: normal; font-size: 10px; font-weight: 600; padding: 1px 6px; border-radius: 6px;
  background: rgba(15,107,92,.12); color: var(--accent);
}
.relation-list small { color: var(--ink-soft); font-size: 12px; flex-shrink: 0; }
.detail-head { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
.back { flex-shrink: 0; width: 44px; min-height: 44px; border: 0; background: transparent; font-size: 26px; line-height: 1; color: var(--accent); padding: 0; }
.detail-title { min-width: 0; display: grid; gap: 2px; }
.detail-title h2 { margin: 0; font-family: var(--font-prose); font-size: 19px; overflow-wrap: anywhere; }
.detail-title small { color: var(--ink-soft); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.actions { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 8px; margin-bottom: 12px; }
.pad { padding: 14px; }
.identity header { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 8px; }
.identity header button { font-size: 12px; padding: 8px 10px; min-height: 44px; }
.warn { color: var(--warn); font-size: 12px; margin: 0 0 8px; }
.seg { display: grid; gap: 2px; padding: 8px 10px; border-left: 3px solid var(--line); margin-bottom: 8px; }
.seg.current { border-left-color: var(--accent); background: rgba(15,107,92,.05); border-radius: 0 8px 8px 0; }
.seg .range { font-size: 11px; color: var(--accent); font-family: ui-monospace, monospace; }
.seg strong { font-size: 13px; }
.seg small { color: var(--ink-soft); font-size: 11px; }
.seg .note { font-style: italic; }
.seg-actions { display: flex; gap: 8px; margin-top: 4px; }
.seg-actions button { font-size: 12px; padding: 8px 10px; min-height: 44px; }
.secret { text-align: center; display: grid; gap: 8px; justify-items: center; }
.secret p { margin: 0; color: var(--ink-soft); font-size: 12px; line-height: 1.7; }
.source-editor { width: 100%; min-height: 60dvh; padding: 12px; border: 1px solid var(--line); border-radius: 12px; font: 13px/1.7 ui-monospace, monospace; background: rgba(255,255,255,.9); }
.state-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 8px; }
.state-card { padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; background: rgba(255,255,255,.5); }
.state-card small { display: block; color: var(--accent); font-size: 10px; margin-bottom: 3px; }
.state-card strong { font-size: 13px; word-break: break-all; }
.gates { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 10px; }
.gate { padding: 4px 9px; border-radius: 8px; font-size: 11px; background: rgba(154,165,160,.12); color: var(--ink-soft); }
.gate[data-state="pass"] { background: rgba(15,107,92,.1); color: var(--accent); }
.gate[data-state="fail"] { background: rgba(163,59,50,.1); color: var(--bad); }
.md-h { font-family: var(--font-prose); margin: 14px 0 8px; font-size: 16px; }
.md-h.h2 { border-bottom: 1px solid var(--line); padding-bottom: 5px; }
.md-h.h3, .md-h.h4 { font-size: 13px; color: var(--accent); }
.md-p { margin: 6px 0; color: var(--ink-soft); font-size: 13px; line-height: 1.8; }
.md-q { margin: 8px 0; padding: 8px 12px; border-left: 3px solid var(--accent); background: rgba(15,107,92,.05); font-family: var(--font-prose); font-size: 14px; border-radius: 0 10px 10px 0; }
.list { margin: 6px 0; padding-left: 18px; }
.list li { color: var(--ink-soft); font-size: 13px; line-height: 1.8; }
.table-wrap { overflow-x: auto; }
.table { width: 100%; border-collapse: collapse; font-size: 12px; }
.table th { text-align: left; color: var(--ink-soft); font-weight: 500; padding: 7px 8px; border-bottom: 1px solid var(--line); white-space: nowrap; }
.table td { padding: 8px; border-bottom: 1px solid var(--line); color: var(--ink-soft); line-height: 1.6; }
.table td:first-child { color: var(--ink); font-weight: 600; }
.sheet-mask { position: fixed; inset: 0; z-index: 60; display: flex; align-items: flex-end; background: rgba(20,35,31,.32); }
.sheet { width: 100%; display: grid; gap: 12px; border-radius: 18px 18px 0 0; padding: 18px 16px calc(18px + env(safe-area-inset-bottom)); max-height: 92dvh; overflow-y: auto; }
.sheet header { display: flex; align-items: center; justify-content: space-between; }
.sheet h2 { margin: 0; font-family: var(--font-prose); font-size: 18px; }
.close { min-width: 44px; min-height: 44px; border: 0; background: transparent; font-size: 22px; line-height: 1; }
.sheet label { display: grid; gap: 6px; font-size: 13px; color: var(--ink-soft); }
.sheet-actions { display: flex; gap: 10px; }
.sheet-actions button { flex: 1; }
</style>
