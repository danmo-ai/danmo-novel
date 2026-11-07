<script setup lang="ts">
/**
 * 桌面书工作台 = 共享逻辑 + 桌面布局。
 *
 * 所有数据与动作都在 useBookWorkspace 里（移动端同一份），这里只留「桌面上怎么排」。
 * 新增业务规则请写进 composable，不要写在这个文件：写在这里的规则移动端拿不到。
 */
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useBookWorkspace, type WorkspaceView } from '@/composables/useBookWorkspace'
import OutlineWorkspace from '@/components/book/OutlineWorkspace.vue'
import BibleWorkspace from '@/components/book/BibleWorkspace.vue'
import ProjectSetup from '@/components/book/ProjectSetup.vue'
import ReviewWorkspace from '@/components/book/ReviewWorkspace.vue'
import ChapterDiff from '@/components/book/ChapterDiff.vue'
import PlanCandidateDiff from '@/components/book/PlanCandidateDiff.vue'
import StructuredOutline from '@/components/book/StructuredOutline.vue'
import UnitProse from '@/components/book/UnitProse.vue'
import UnitContract from '@/components/book/UnitContract.vue'
import UnitFlowActions from '@/components/book/UnitFlowActions.vue'
import RunConfirmDialog from '@/components/book/RunConfirmDialog.vue'
import { runKindLabel } from '@/utils/runLabels'
import { callCacheText, hitRateText } from '@/utils/tokenStats'

const route = useRoute()
const bookId = computed(() => route.params.id as string)
// 桌面用顶部 Tab 切换面板；移动端把它解释成路由层级，语义一致。
const view = ref<WorkspaceView>('write')

const {
  store,
  selectedUnit, editorPath, editorContent, saving, busy, runDrawer, showUnitPicker,
  editingProse, editingOutline,
  confirmSpec, confirmTitle,
  listedCandidates, isProseUnit, showSetupTab, volumeGroups, primaryInfo,
  stageLabels, statusText, chapterText,
  unverifiedEvidence, runCacheOf, toggleVolume, isCollapsed,
  saveFile, beginChapterEdit, cancelChapterEdit, saveChapterEdit, saveOutlineField,
  reviewStructure, repairStructure, start, startIdea, startRewrite, startPlan,
  planCandidateFiles, planModeLabel, candidateTarget, candidateStructure, planCastPreview,
  confirmRun, primaryAction, runUnitAction, accept, reviseFindings,
  focusUnit, rejectUnitFindings, retrySplitReview, reject, enableWrite, defaultModelFor,
} = useBookWorkspace({ bookId, view })
</script>

<template>
  <div v-if="store.currentBook" class="book-page">
    <div class="book-head">
      <div>
        <h1>{{ store.currentBook.summary.title }}</h1>
      </div>
      <div class="book-head-actions">
        <button v-if="store.currentBook.summary.readOnly" @click="enableWrite">启用写入</button>
      </div>
    </div>

    <div class="primary-task panel">
      <div class="primary-task-copy">
        <span class="task-kicker">下一步 · {{ primaryInfo.target }}</span>
        <p>{{ primaryInfo.description }}</p>
      </div>
      <button class="primary" :disabled="busy" @click="primaryAction">{{ busy ? '正在启动…' : primaryInfo.label }}</button>
    </div>

    <div class="tabs" role="tablist" aria-label="创作主导航">
      <button v-if="showSetupTab" role="tab" :aria-selected="view === 'setup'" :class="{ active: view === 'setup' }" @click="view = 'setup'">立项</button>
      <button role="tab" :aria-selected="view === 'write'" :class="{ active: view === 'write' }" @click="view = 'write'">写作</button>
      <button role="tab" :aria-selected="view === 'outline'" :class="{ active: view === 'outline' }" @click="view = 'outline'">大纲</button>
      <button role="tab" :aria-selected="view === 'bible'" :class="{ active: view === 'bible' }" @click="view = 'bible'">设定</button>
      <button role="tab" :aria-selected="view === 'review'" :class="{ active: view === 'review' }" @click="view = 'review'">审校</button>
    </div>

    <div v-if="view === 'write'" class="mobile-workspace-tools">
      <button class="unit-picker-toggle panel" :aria-expanded="showUnitPicker" @click="showUnitPicker = !showUnitPicker">
        <span class="unit-picker-label">
          <small>当前单元</small>
          <strong>{{ selectedUnit?.unitId || '选择单元' }}</strong>
          <span v-if="selectedUnit?.title">{{ selectedUnit.title }}</span>
        </span>
        <span class="unit-picker-caret">{{ showUnitPicker ? '收起' : '切换' }}</span>
      </button>
    </div>

    <div class="workspace" :class="[`view-${view}`, { 'show-unit-picker': showUnitPicker }]">
      <aside v-if="view === 'write'" class="unit-tree panel">
        <div class="tree-head"><span>单元</span><span class="tree-count">{{ store.currentBook.units.length }}</span></div>
        <div v-for="g in volumeGroups" :key="g.volume" class="volume-group">
          <button class="volume-head" @click="toggleVolume(g.volume)">
            <span class="volume-caret">{{ isCollapsed(g.volume) ? '▸' : '▾' }}</span>
            <span class="volume-label">第 {{ g.volume }} 卷</span>
            <span class="volume-progress">{{ g.reviewed }}/{{ g.units.length }}</span>
          </button>
          <p v-if="g.title && !isCollapsed(g.volume)" class="volume-title">{{ g.title }}</p>
          <template v-if="!isCollapsed(g.volume)">
            <button v-for="u in g.units" :key="u.unitId"
              class="unit-row" :class="{ active: selectedUnit?.unitId === u.unitId }"
              @click="selectedUnit = u">
              <span class="dot" :data-status="u.status"></span>
              <span class="unit-main">
                <span class="unit-line"><span class="unit-id">{{ u.unitId }}</span><span class="unit-status" :data-status="u.status">{{ statusText[u.status] || u.status }}</span></span>
                <span class="unit-title">{{ u.title || '未完成单元合同' }}</span>
              </span>
            </button>
          </template>
        </div>
        <div v-if="!(store.currentBook.units || []).length" class="tree-empty">还没有单元。先到大纲页完成「立项」与「生成卷纲」。</div>
      </aside>

      <ProjectSetup
        v-if="view === 'setup' && showSetupTab"
        :busy="busy"
        :saving="saving"
        @save="saveFile"
        @idea="startIdea"
        @plan="(mode, volume) => startPlan(mode, volume)"
        @accept="accept"
        @reject="reject"
      />
      <OutlineWorkspace
        v-else-if="view === 'outline'"
        :saving="saving"
        :busy="busy"
        @open-unit="selectedUnit = $event; view = 'write'"
        @save="saveFile"
        @generate="start"
        @save-field="saveOutlineField"
        @structure-review="reviewStructure"
        @plan="startPlan"
      />
      <BibleWorkspace
        v-else-if="view === 'bible'"
        :saving="saving"
        @save="saveFile"
        @open-unit="selectedUnit = $event; view = 'write'"
        @rewrite="startRewrite"
      />
      <ReviewWorkspace
        v-else-if="view === 'review'"
        :selected-unit="selectedUnit"
        :busy="busy"
        :read-only="store.currentBook.summary.readOnly"
        @revise="reviseFindings"
        @retry-split="retrySplitReview"
        @focus-unit="focusUnit"
        @reject-unit="rejectUnitFindings"
        @accept="accept"
        @reject-candidate="reject"
      />
      <section v-else class="editor-panel panel">
        <template v-if="isProseUnit && selectedUnit">
          <UnitProse v-if="!editingProse" :unit="selectedUnit" :raw="editorContent"
            :can-write="!store.currentBook.summary.readOnly" @edit="beginChapterEdit">
            <template #contract>
              <details class="inline-contract">
                <summary>单元合同 · 写正文时对照</summary>
                <UnitContract :unit="selectedUnit" compact />
              </details>
            </template>
            <template #actions>
              <UnitFlowActions
                :unit="selectedUnit"
                :busy="busy"
                :read-only="store.currentBook.summary.readOnly"
                emphasize
                @run="runUnitAction"
              />
            </template>
          </UnitProse>
          <div v-else>
            <div class="editor-toolbar">
              <span class="path">{{ editorPath }}</span>
              <div class="editor-actions">
                <button @click="saveChapterEdit" :disabled="saving">{{ saving ? '保存中' : '保存' }}</button>
                <button @click="cancelChapterEdit">取消</button>
              </div>
            </div>
            <textarea v-model="editorContent" class="prose-editor" spellcheck="false"></textarea>
          </div>
        </template>
        <template v-else-if="selectedUnit">
          <!-- 无正文单元：写作页直接生产/查看细纲 -->
          <div class="unit-work">
            <header class="unit-work-head">
              <div>
                <small>{{ selectedUnit.unitId }} · {{ statusText[selectedUnit.status] || selectedUnit.status }}<template v-if="selectedUnit.chapterFrom"> · 第 {{ selectedUnit.chapterFrom }}–{{ selectedUnit.chapterTo }} 章</template></small>
                <h3>{{ selectedUnit.title || '未完成单元合同' }}</h3>
              </div>
              <div class="unit-work-actions">
                <UnitFlowActions
                  v-if="!editingOutline"
                  :unit="selectedUnit"
                  :busy="busy"
                  :read-only="store.currentBook.summary.readOnly"
                  emphasize
                  @run="runUnitAction"
                />
                <button v-if="!editingOutline" @click="editingOutline = true">手工编辑细纲</button>
              </div>
            </header>
            <template v-if="!editingOutline">
              <UnitContract :unit="selectedUnit" />
              <details class="raw-outline">
                <summary>查看细纲源文件</summary>
                <pre>{{ editorContent }}</pre>
              </details>
            </template>
            <div v-else class="outline-edit">
              <div class="editor-toolbar">
                <span class="path">{{ selectedUnit.outlinePath }}</span>
                <div class="editor-actions">
                  <button @click="saveFile(); editingOutline = false" :disabled="saving">{{ saving ? '保存中' : '保存' }}</button>
                  <button @click="editingOutline = false">取消</button>
                </div>
              </div>
              <textarea v-model="editorContent" class="prose-editor" spellcheck="false"></textarea>
            </div>
          </div>
        </template>
        <template v-else>
          <div class="editor-toolbar">
            <span class="path">{{ editorPath }}</span>
            <div class="editor-actions">
              <button @click="saveFile()" :disabled="saving">{{ saving ? '保存中' : '保存' }}</button>
            </div>
          </div>
          <textarea v-model="editorContent" class="prose-editor" spellcheck="false"></textarea>
        </template>
      </section>

    </div>

    <section v-if="listedCandidates.length" class="candidates">
      <h2>待审候选</h2>
      <!--
        只列「接受后会写文件」的候选。审校清单不在这里：它不写正文，唯一归属地是
        审校页。修订候选在审校页按单元就地展开 diff（清单区切换成当前这一步），
        所以这里只在其他页面列它，避免同一份 diff 在页面首尾各出现一次。
        也不放"另有 N 条审校结论"的跳转提示——那既不能处理问题，又让人以为清单
        丢了一份，属于噪音；整块为空时直接不渲染。
      -->
      <article v-for="c in listedCandidates" :key="c.id" class="candidate panel">
        <header>
          <div>
            <strong>{{ c.kind === 'plan' ? planModeLabel(c.mode) : (c.kind === 'write' && c.baseChapters?.length ? '正文 · 原文优化' : (stageLabels[c.kind] || c.kind)) }} · {{ candidateTarget(c) }}</strong>
            <span class="muted"> · {{ new Date(c.createdAt).toLocaleString() }}</span>
          </div>
          <div>
            <button v-if="c.kind === 'structure_review' && c.findings?.some((f) => f.severity === 'blocking')" class="primary" @click="repairStructure(c)">按问题修复</button>
            <button @click="reject(c)">拒绝</button>
            <button class="primary" @click="accept(c)">{{ c.kind === 'revise' ? '确认落盘' : '接受' }}</button>
          </div>
        </header>
        <p v-if="c.kind === 'structure_review'" class="muted">结构编辑只审查父级继承、因果、兑现和状态。按问题修复会生成新的文档候选，接受后才会写回 Markdown。</p>
        <!-- 修订候选：先看 diff 再落盘，接受不能是盲签。 -->
        <template v-else-if="c.kind === 'revise'">
          <p class="muted">下面是相对现有正文的改动。确认无误后点「确认落盘」覆盖正文，这次修订即告完成；落盘后旧正文清单按版本归档，不再把已勾选等同于已修复；需要时请按新正文重新审校。</p>
          <ChapterDiff :base="c.baseChapters" :revised="c.chapters || []" />
        </template>
        <template v-else-if="c.kind === 'finalize' && c.actualChapters?.length">
          <p class="muted">逐章核对实际正文；只有作者接受后，以下结果才进入连续性账本。</p>
          <!-- 回证未命中不是错误：引文抄错字形不等于迁移错了。但必须让作者看见，
               否则他会以为每条迁移都对照过正文。 -->
          <p v-if="unverifiedEvidence(c).length" class="warn-text">
            有 {{ unverifiedEvidence(c).length }} 条状态迁移的回证在本章正文里找不到（下面标出）。接受时会记入定稿日志；
            请对照正文确认 from → to 是否成立，不确定就先不接受。
          </p>
          <details v-for="ch in c.actualChapters" :key="ch.chapter" open>
            <summary>第 {{ ch.chapter }} 章 · {{ ch.summary }}</summary>
            <ul class="plan-files">
              <li v-for="d in ch.stateDeltas || []" :key="`${d.stem}-${d.field}-${d.evidence}`">{{ d.stem }} · {{ d.field }}：{{ d.from }} → {{ d.to }}（{{ d.evidence }}）</li>
              <li v-if="ch.plant?.length">实际埋下：{{ ch.plant.join('、') }}</li>
              <li v-if="ch.payoff?.length">实际回收：{{ ch.payoff.join('、') }}</li>
            </ul>
          </details>
        </template>
        <!-- 优化模式的重写候选同样先看 diff：作者要判断的是"优化了什么"。 -->
        <template v-else-if="c.kind === 'write' && c.baseChapters?.length">
          <p class="muted">下面是相对原稿的改动。接受后才会覆盖正文；若几乎没改、或改得像重写，候选里会有提示，可据此换「重新生成」或改走审校后的自动修复。</p>
          <ChapterDiff :base="c.baseChapters" :revised="c.chapters || []" />
        </template>
        <pre v-else-if="c.chapters" class="candidate-prose">{{ chapterText(c) }}</pre>
        <pre v-else-if="c.unitOutlines" class="candidate-prose">{{ JSON.stringify(c.unitOutlines, null, 2) }}</pre>
        <template v-else-if="c.kind === 'idea'">
          <p class="muted">自由格式的创意提案：接受后只写 outline/idea.md，不写总纲、设定或人物。</p>
          <pre class="candidate-prose">{{ c.replacement }}</pre>
        </template>
        <pre v-else-if="c.replacement" class="candidate-prose">{{ c.replacement }}</pre>
        <div v-else-if="c.planMarkdown || c.planSettings" class="plan-candidate">
          <p class="plan-mode"><strong>{{ planModeLabel(c.mode) }}</strong><span class="muted"> · 接受后写入 {{ planCandidateFiles(c).length }} 个文件</span></p>
          <template v-if="c.mode === 'volume'">
            <details v-if="c.planMarkdown?.volume_outline" open><summary>卷纲 · {{ c.volumeId }}</summary><StructuredOutline v-if="candidateStructure(c, 'volume') && !candidateStructure(c, 'volume')!.legacy" :document="candidateStructure(c, 'volume')!" compact read-only /><pre v-else class="candidate-prose">{{ c.planMarkdown.volume_outline }}</pre></details>
            <p class="muted">只写当前卷纲，并播种索引中尚不存在的单元；总纲/设定/人物/正文不变。</p>
          </template>
          <template v-else>
            <details v-if="c.planMarkdown?.book_outline" open><summary>总纲</summary><StructuredOutline v-if="candidateStructure(c, 'book') && !candidateStructure(c, 'book')!.legacy" :document="candidateStructure(c, 'book')!" compact read-only /><pre v-else class="candidate-prose">{{ c.planMarkdown.book_outline }}</pre></details>
            <details v-if="c.planSettings && Object.keys(c.planSettings).length"><summary>设定 · {{ Object.keys(c.planSettings).length }} 项</summary>
              <ul class="plan-files"><li v-for="p in Object.keys(c.planSettings)" :key="p">{{ p }}</li></ul>
            </details>
            <details v-if="c.planMarkdown?.cast_json"><summary>人物</summary><pre class="candidate-prose">{{ planCastPreview(c) }}</pre></details>
            <p class="muted">立项只写总纲派生设定与人物；不写卷纲、不播种单元。</p>
          </template>
          <p v-if="(c as any).preservePaths?.length" class="muted">保留不覆盖：{{ ((c as any).preservePaths as string[]).join('、') }}</p>
          <details open><summary>变更预览（相对当前 canon）</summary><PlanCandidateDiff :candidate="c" :files="store.currentBook?.files || {}" /></details>
          <details><summary>将覆盖的文件（已排除保留项）</summary><ul class="plan-files"><li v-for="p in planCandidateFiles(c)" :key="p">{{ p }}</li></ul></details>
        </div>
        <div v-if="c.findings?.length" class="candidate-findings">
          <div v-for="f in c.findings.slice(0, 6)" :key="f.id" class="finding" :class="f.severity">
            {{ f.title }} <span v-if="f.evidence">· {{ f.evidence }}</span>
          </div>
        </div>
      </article>
    </section>

    <RunConfirmDialog v-if="confirmSpec" :spec="confirmSpec" :title="confirmTitle"
      :model-label="defaultModelFor(confirmSpec.kind === 'plan' || confirmSpec.kind === 'rewrite_entity' ? 'plan' : confirmSpec.kind)"
      @confirm="confirmRun" @cancel="confirmSpec = null" />

    <div v-if="runDrawer" class="run-drawer panel">
      <header><strong>模型日志</strong><button @click="runDrawer = false">×</button></header>
      <div v-if="store.activeRun">
        <p>{{ runKindLabel(store.activeRun.kind, store.activeRun.spec?.rewriteMode || store.activeRun.spec?.mode) }} · {{ store.activeRun.status }}</p>
        <p class="muted">模型 {{ store.activeRun.modelId }} · 调用 {{ store.activeRun.callsUsed }}/{{ store.activeRun.policy?.maxCalls || '计算中' }}</p>
        <p v-if="runCacheOf(store.activeRun).hasData" class="muted">
          缓存命中 {{ runCacheOf(store.activeRun).read.toLocaleString() }} token（{{ hitRateText(runCacheOf(store.activeRun)) }}）
          <template v-if="runCacheOf(store.activeRun).creation">· 写入 {{ runCacheOf(store.activeRun).creation.toLocaleString() }}</template>
        </p>
        <div v-for="c in store.activeRun.calls" :key="c.index" class="call-row">
          #{{ c.index }} {{ c.step }} · in {{ c.promptTokens }} / out {{ c.completionTokens }} · {{ c.durationMs }}ms
          <template v-if="c.cacheReadTokens"> · 缓存 {{ callCacheText(c) }}</template>
        </div>
        <p v-for="w in store.activeRun.warnings || []" :key="w" class="warn-text">{{ w }}</p>
        <p v-if="store.activeRun.error" class="error-text">{{ store.activeRun.error }}</p>
      </div>
      <div v-else class="muted">暂无运行</div>
    </div>
  </div>
</template>

<style scoped>
/* ---- 页头 ---- */
.book-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 16px; padding: 4px 0 18px; }
h1 { font-family: var(--font-prose); margin: 6px 0 0; font-size: 30px; line-height: 1.25; }
.book-head-actions { display: flex; gap: 8px; align-items: center; flex-shrink: 0; }

/* ---- 主任务条 ---- */
.primary-task { display: flex; justify-content: space-between; align-items: center; gap: 20px; padding: 14px 16px 14px 20px; margin-bottom: 16px; border-color: rgba(15,107,92,.2); background: linear-gradient(120deg, rgba(15,107,92,.06), rgba(255,255,255,.75)); }
.primary-task-copy { min-width: 0; display: grid; gap: 3px; }
.task-kicker { color: var(--accent); font-size: 12px; font-weight: 700; }
.primary-task-copy p { margin: 0; color: var(--ink-soft); font-size: 12px; }
.primary-task .primary { flex-shrink: 0; padding: 10px 18px; }

/* ---- 主 Tab ---- */
.tabs { display: inline-flex; gap: 2px; margin-bottom: 18px; padding: 4px; border-radius: 12px; background: rgba(20,35,31,.05); }
.tabs button { border: none; background: transparent; color: var(--ink-soft); padding: 8px 22px; border-radius: 9px; font-weight: 600; }
.tabs button:hover:not(:disabled) { background: rgba(255,255,255,.7); color: var(--ink); }
.tabs button.active { background: white; color: var(--accent); box-shadow: 0 2px 8px rgba(20,35,31,.1); }

/* 手机工作区：当前单元选择器和上下文按需提供，正文优先。 */
.mobile-workspace-tools { display: none; }
.unit-picker-label { display: grid; gap: 3px; min-width: 0; text-align: left; }
.unit-picker-label small { color: var(--ink-soft); font-size: 10px; }
.unit-picker-label strong { color: var(--accent); font-size: 14px; }
.unit-picker-label > span { color: var(--ink-soft); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.unit-picker-caret { color: var(--accent); font-size: 12px; flex-shrink: 0; }

/* ---- 三栏工作区 ---- */
.workspace { display: grid; grid-template-columns: 236px minmax(0, 1fr); gap: 18px; align-items: start; }
.workspace:not(.view-write) { grid-template-columns: minmax(0, 1fr); }

/* 左：单元树 */
.unit-tree { padding: 10px; max-height: 74vh; overflow-y: auto; position: sticky; top: 12px; }
.tree-head { display: flex; justify-content: space-between; align-items: center; font-size: 12px; font-weight: 600; color: var(--ink-soft); padding: 6px 10px 10px; }
.tree-count { font-size: 11px; background: rgba(20,35,31,.06); padding: 1px 8px; border-radius: 20px; }
.unit-row { display: flex; align-items: flex-start; gap: 8px; width: 100%; border: none; background: transparent; padding: 9px 10px; border-radius: 10px; text-align: left; margin-bottom: 2px; }
.unit-row:hover:not(:disabled) { background: rgba(15,107,92,.06); color: inherit; }
.unit-row.active { background: rgba(15,107,92,.1); }
.unit-row.active .unit-id { color: var(--accent); }
.unit-main { display: grid; gap: 3px; min-width: 0; flex: 1; }
.unit-line { display: flex; justify-content: space-between; align-items: center; gap: 6px; }
.unit-id { font-size: 12px; font-weight: 600; white-space: nowrap; }
.unit-title { font-size: 11px; color: var(--ink-soft); line-height: 1.4; overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 1; -webkit-box-orient: vertical; }
.unit-status { font-size: 10px; padding: 1px 7px; border-radius: 20px; background: rgba(20,35,31,.06); color: var(--ink-soft); white-space: nowrap; flex-shrink: 0; }
.unit-status[data-status="reviewed"], .unit-status[data-status="finalized"] { background: rgba(31,122,77,.12); color: var(--ok); }
.unit-status[data-status="drafted"] { background: rgba(161,92,18,.12); color: var(--warn); }
.unit-status[data-status="accepted"] { background: rgba(15,107,92,.12); color: var(--accent); }
.dot { width: 7px; height: 7px; border-radius: 50%; background: #9aa; flex-shrink: 0; margin-top: 5px; }
.dot[data-status="reviewed"], .dot[data-status="finalized"] { background: var(--ok); }
.dot[data-status="drafted"] { background: var(--warn); }
.dot[data-status="accepted"] { background: var(--accent); }
.tree-empty { color: var(--ink-soft); font-size: 12px; padding: 16px 10px; }
/* 卷分组 */
.volume-group { margin-bottom: 6px; }
.volume-head { display: flex; align-items: center; gap: 6px; width: 100%; border: none; background: transparent; padding: 8px 10px 6px; border-radius: 8px; text-align: left; }
.volume-head:hover:not(:disabled) { background: rgba(15,107,92,.06); }
.volume-caret { color: var(--ink-soft); font-size: 11px; width: 12px; }
.volume-label { font-size: 12px; font-weight: 700; color: var(--ink); }
.volume-progress { margin-left: auto; font-size: 10px; color: var(--ok); background: rgba(31,122,77,.1); padding: 1px 8px; border-radius: 20px; }
.volume-title { margin: 0 10px 6px 28px; font-size: 10px; color: var(--ink-soft); line-height: 1.5; overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 1; -webkit-box-orient: vertical; }
.volume-group .unit-row { margin-left: 6px; }
/* 单元工作流引导：侧栏窄，必须单行 */
/* 中：正文/编辑器 */
.editor-panel { min-height: 60vh; overflow: hidden; }
.editor-toolbar { display: flex; justify-content: space-between; align-items: center; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.path { font-size: 12px; color: var(--ink-soft); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.editor-actions { display: flex; gap: 8px; flex-wrap: wrap; flex-shrink: 0; }
.prose-editor {
  width: 100%; min-height: 64vh; border: none; resize: vertical; padding: 24px 28px;
  font-family: var(--font-prose); font-size: 17px; line-height: 1.9; color: var(--ink);
  background: transparent; outline: none;
}

/* 无正文单元：细纲工作区 */
.unit-work { padding: 24px 26px; display: grid; gap: 20px; }
.unit-work-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 16px; }
.unit-work-head small { color: var(--accent); font-size: 11px; font-weight: 600; }
.unit-work-head h3 { margin: 6px 0 0; font-family: var(--font-prose); font-size: 24px; }
.unit-work-actions { display: flex; gap: 8px; flex-shrink: 0; flex-wrap: wrap; }
.raw-outline { border-top: 1px solid var(--line); padding-top: 4px; }
.raw-outline summary { padding: 10px 0; cursor: pointer; color: var(--ink-soft); font-size: 12px; }
.raw-outline pre { white-space: pre-wrap; font: 12px/1.7 ui-monospace, monospace; color: var(--ink-soft); background: rgba(255,255,255,.5); padding: 14px; border-radius: 10px; max-height: 320px; overflow: auto; }
.inline-contract { border-bottom: 1px solid var(--line); padding-bottom: 16px; }
.inline-contract summary { cursor: pointer; color: var(--accent); font-size: 12px; font-weight: 600; padding: 4px 0; }
.inline-contract[open] summary { margin-bottom: 14px; }

/* 待审候选 */
.candidates { margin-top: 28px; }
.candidates h2 { font-size: 17px; font-family: var(--font-prose); margin: 0 0 10px; }
.muted { color: var(--ink-soft); font-size: 12px; }
.candidate { padding: 18px; margin-bottom: 14px; }
.candidate header { display: flex; justify-content: space-between; align-items: center; gap: 10px; margin-bottom: 12px; }
.candidate header > div:last-child { display: flex; gap: 8px; flex-shrink: 0; }
.candidate-prose { white-space: pre-wrap; font-family: var(--font-prose); font-size: 14px; line-height: 1.9; max-height: 300px; overflow: auto; background: rgba(255,255,255,.5); padding: 16px; border-radius: 12px; border: 1px solid var(--line); margin: 0; }
.plan-candidate { display: grid; gap: 8px; }
.plan-mode { margin: 0; font-size: 13px; }
.plan-candidate details { border: 1px solid var(--line); border-radius: 10px; padding: 8px 12px; background: rgba(255,255,255,.4); }
.plan-candidate summary { cursor: pointer; font-size: 13px; font-weight: 600; }
.plan-files { margin: 8px 0 0; padding-left: 18px; font-size: 12px; color: var(--ink-soft); }
.finding { padding: 8px 12px; margin: 8px 0 0; border-radius: 10px; background: rgba(161,92,18,.08); font-size: 12px; }
.finding.blocking { background: rgba(163,59,50,.1); color: var(--bad); }

/* 运行抽屉 */
.run-drawer { position: fixed; right: 24px; bottom: 24px; width: min(400px, 92vw); padding: 18px; z-index: 30; }
.run-drawer header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.run-drawer p { margin: 6px 0; font-size: 13px; }
.call-row { font-size: 12px; padding: 8px 0; border-top: 1px solid var(--line); color: var(--ink-soft); }
.error-text { color: var(--bad); font-size: 12px; }
/* 退路提示：跑成了但多花了钱，措辞要比错误轻、比普通说明显眼。 */
.warn-text { color: var(--warn, #b7791f); font-size: 12px; }

@media (max-width: 1180px) {
  .workspace.view-write { grid-template-columns: 210px minmax(0, 1fr); }
}
@media (max-width: 720px) {
  .book-head { padding-bottom: 12px; }
  .book-head h1 { font-size: 24px; }
  .tabs { display: flex; width: 100%; overflow-x: auto; margin-bottom: 12px; scrollbar-width: none; }
  .tabs::-webkit-scrollbar { display: none; }
  .tabs button { flex: 1 0 auto; min-width: 64px; min-height: 40px; padding: 8px 16px; }
  .mobile-workspace-tools { display: grid; gap: 8px; margin-bottom: 12px; }
  .unit-picker-toggle { display: flex; align-items: center; justify-content: space-between; gap: 12px; width: 100%; padding: 12px 14px; text-align: left; }
  .workspace, .workspace.view-write { display: grid; grid-template-columns: minmax(0, 1fr); width: 100%; min-width: 0; }
  .workspace > * { min-width: 0; width: 100%; }
  .editor-panel { min-width: 0; width: 100%; overflow: hidden; }
  .unit-work { min-width: 0; width: 100%; }
  .workspace.view-write:not(.show-unit-picker) .unit-tree { display: none; }
  .workspace.view-write.show-unit-picker .unit-tree { display: block; max-height: 55dvh; overflow-y: auto; }
  .book-head { flex-direction: column; align-items: flex-start; gap: 10px; }
  .unit-tree { max-height: 240px; position: static; }
  .workspace.view-write > .unit-tree { grid-row: 1; }
  .workspace.view-write > .editor-panel { grid-row: 2; }
  .editor-panel { min-height: 0; }
  .prose-editor { min-height: 48vh; padding: 18px 16px; font-size: 16px; }
  .editor-toolbar { align-items: flex-start; flex-direction: column; }
  .candidate { padding: 14px; }
  .candidate header { flex-direction: column; align-items: stretch; }
  .candidate header > div:last-child { flex-wrap: wrap; }
  .candidate header button { flex: 1; }
  .candidate-prose { max-height: none; padding: 14px; font-size: 16px; }
  .unit-tree { max-height: 40dvh; overflow: auto; }
  .unit-work { padding: 16px 12px; min-width: 0; }
  .unit-work-head { display: grid; grid-template-columns: minmax(0, 1fr); gap: 14px; align-items: stretch; }
  .unit-work-head > div:first-child { min-width: 0; }
  .unit-work-head h3 { overflow-wrap: anywhere; font-size: 22px; }
  .unit-work-actions { display: grid; gap: 10px; width: 100%; min-width: 0; }
  .unit-work-actions > * { width: 100%; min-width: 0; }
  .unit-work-actions > button { width: 100%; min-height: 44px; }
  .unit-work-actions :deep(.unit-flow-actions) { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); width: 100%; gap: 8px; }
  .unit-work-actions :deep(.unit-flow-actions button) { width: 100%; min-width: 0; min-height: 44px; white-space: normal; padding: 10px 12px; }
  .editor-actions { width: 100%; }
  .editor-actions button { flex: 1; }
  .run-drawer { inset: auto 0 0; width: 100%; max-height: 72dvh; overflow: auto; border-radius: 18px 18px 0 0; padding-bottom: max(18px, env(safe-area-inset-bottom)); }
  .primary-task { align-items: stretch; flex-direction: column; gap: 12px; }
  .primary-task .primary { width: 100%; }
}
</style>
