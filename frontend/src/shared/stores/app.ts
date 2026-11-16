import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/shared/api/client'
import type { BookSnapshot, BookSummary, Candidate, LLMModel, ProviderPublic, Run } from '@/shared/types'

export const useAppStore = defineStore('app', () => {
  const books = ref<BookSummary[]>([])
  const currentBook = ref<BookSnapshot | null>(null)
  const primary = ref<{ kind: string; unitId?: string; volume: number } | null>(null)
  const candidates = ref<Candidate[]>([])
  /** 已终结候选（accepted/rejected/expired）：接受归档后的审校清单靠它回看。 */
  const candidateHistory = ref<Candidate[]>([])
  const runs = ref<Run[]>([])
  const activeRun = ref<Run | null>(null)
  const providers = ref<ProviderPublic[]>([])
  const models = ref<LLMModel[]>([])
  const presets = ref<any[]>([])
  const assignments = ref<{ role: string; modelId: string }[]>([])
  const toast = ref('')
  const error = ref('')
  /** 书架取数状态：失败时保留上一次的书，绝不把「请求失败」显示成「书架是空的」。 */
  const booksLoading = ref(false)
  const booksError = ref('')
  /** 连续性状态摘要：已建到哪个单元、多少人物状态、多少未回收伏笔。 */
  const continuity = ref<{ units: number; lastUnit?: string; afterCh?: number; cast: number; openLoops: number } | null>(null)

  // ---- 渐进式取数 ----
  // 首屏只拉骨架（书端点默认即精简）；大纲/设定面板的文件按需加载（include=files），
  // 候选正文与历史归档在展开时才取。文件加载按 revision 失效。
  const filesLoadedRevision = ref('')
  let filesPromise: Promise<void> | null = null
  const historyLoaded = ref(false)
  let historyPromise: Promise<void> | null = null
  const candidateDetails = ref<Record<string, Candidate>>({})
  const detailPromises: Record<string, Promise<Candidate | null>> = {}

  function note(msg: string) { toast.value = msg; setTimeout(() => { if (toast.value === msg) toast.value = '' }, 4000) }
  function fail(e: any) { error.value = e?.message || String(e); setTimeout(() => { if (error.value) error.value = '' }, 8000) }

  async function loadBooks() {
    booksLoading.value = true
    booksError.value = ''
    try {
      books.value = (await api.get<BookSummary[] | null>('/api/books')) ?? []
    } catch (e: any) {
      // Keep the previous shelf: an empty result must mean "no books", never
      // "the request failed". The shelf shows booksError with a retry instead.
      booksError.value = e?.message || String(e)
      throw e
    } finally {
      booksLoading.value = false
    }
  }

  /** 第一层：导航骨架 + 摘要级活动。不含面板文件、候选正文与历史归档。 */
  async function loadBook(id: string) {
    type StateSummary = { units: number; lastUnit?: string; afterCh?: number; cast: number; openLoops: number }
    const res = await api.get<{ book: BookSnapshot; primary: any; continuity?: StateSummary }>(`/api/books/${id}`)
    // 已加载的面板文件跨骨架刷新保留（同 revision 时）。
    const previous = currentBook.value
    if (previous && previous.summary.id === id && filesLoadedRevision.value === previous.revision) {
      res.book = {
        ...res.book,
        stateRaw: previous.stateRaw,
        files: previous.files,
        structuredOutlines: previous.structuredOutlines,
        identitySegments: previous.identitySegments,
        volumes: res.book.volumes?.length ? res.book.volumes : previous.volumes,
        cast: (res.book.cast || []).map((card) => ({
          ...card,
          raw: previous.cast?.find((c) => c.stem === card.stem)?.raw ?? card.raw,
        })),
      }
    } else {
      filesLoadedRevision.value = ''
    }
    currentBook.value = res.book
    primary.value = res.primary
    continuity.value = res.continuity ?? null
    await Promise.all([loadCandidates(id), loadRuns(id)])
    pruneCandidateDetails()
    if (historyLoaded.value) await loadCandidateHistory(id)
    // 面板文件已在屏上时跟随新 revision 重取。
    if (filesLoadedRevision.value) await loadBookFiles(id)
  }

  /** 第二层：大纲/设定面板文档。幂等（按 revision），并发共享一个请求。 */
  async function loadBookFiles(id: string) {
    const book = currentBook.value
    if (!book || book.summary.id !== id) return
    if (filesLoadedRevision.value === book.revision) return
    if (!filesPromise) {
      filesPromise = (async () => {
        try {
          const res = await api.get<{ book: BookSnapshot; primary: any; continuity?: any }>(`/api/books/${id}?include=files`)
          // 只采纳与当前骨架同 revision 的响应；否则下次调用重取。
          if (currentBook.value?.revision === res.book.revision) {
            currentBook.value = res.book
            primary.value = res.primary
            continuity.value = res.continuity ?? null
            filesLoadedRevision.value = res.book.revision
          }
        } finally {
          filesPromise = null
        }
      })()
    }
    // 必须 await 局部引用：共享的 filesPromise 会在请求自己的 finally 里被清空，
    // 并发调用若直接 await filesPromise，可能拿到已 settle 的旧 Promise 立即返回，
    // 而文件尚未写进 currentBook——面板渲染成空白，且 revision 随后被标记为已加载，
    // 后续调用全部跳过重取，空白永久化。局部引用保证等到的是本次请求真正完成。
    const pending = filesPromise
    await pending
  }

  const filesReady = () => !!currentBook.value && filesLoadedRevision.value === currentBook.value.revision

  async function loadCandidates(id: string) {
    candidates.value = (await api.get<Candidate[] | null>(`/api/books/${id}/candidates`)) ?? []
  }
  async function loadCandidateHistory(id: string) {
    if (!historyPromise) {
      historyPromise = (async () => {
        try {
          candidateHistory.value = (await api.get<Candidate[] | null>(`/api/books/${id}/candidates/history?kind=review`)) ?? []
          historyLoaded.value = true
        } finally {
          historyPromise = null
        }
      })()
    }
    await historyPromise
  }
  async function loadRuns(id: string) {
    runs.value = (await api.get<Run[] | null>(`/api/books/${id}/runs?limit=50`)) ?? []
  }

  /** 第三层：单个候选的完整正文，展开卡片时取，缓存到候选离开待审列表。 */
  async function candidateDetail(id: string): Promise<Candidate | null> {
    const cached = candidateDetails.value[id]
    if (cached) return cached
    if (!detailPromises[id]) {
      detailPromises[id] = (async () => {
        try {
          const full = await api.get<Candidate | null>(`/api/candidates/${id}`)
          if (full) candidateDetails.value = { ...candidateDetails.value, [id]: full }
          return full
        } catch {
          return null
        } finally {
          delete detailPromises[id]
        }
      })()
    }
    return detailPromises[id]
  }
  function resolvedCandidate(c: Candidate): Candidate { return candidateDetails.value[c.id] ?? c }
  function pruneCandidateDetails() {
    const open = new Set(candidates.value.map((c) => c.id))
    const kept: Record<string, Candidate> = {}
    for (const [id, detail] of Object.entries(candidateDetails.value)) {
      if (open.has(id)) kept[id] = detail
    }
    candidateDetails.value = kept
  }

  async function loadLLM() {
    const [p, m, pr, s] = await Promise.all([
      api.get<ProviderPublic[]>('/api/llm/configs'),
      api.get<LLMModel[]>('/api/llm/models'),
      api.get<any[]>('/api/llm/presets'),
      api.get<any>('/api/settings'),
    ])
    providers.value = p || []
    models.value = m || []
    presets.value = pr || []
    assignments.value = s.modelAssignments || []
  }
  async function startRun(spec: any) {
    const run = await api.post<Run>('/api/runs', spec)
    activeRun.value = run
    note('任务已开始：' + spec.kind)
    return run
  }
  async function pollRun(id: string) {
    const run = await api.get<Run>(`/api/runs/${id}`)
    activeRun.value = run
    return run
  }
  function clearActiveRun() { activeRun.value = null }
  async function acceptCandidate(
    id: string,
    baseRevision: string,
    opts: { forceRiskAccept?: boolean } = {},
  ) {
    const c = await api.post<Candidate>(`/api/candidates/${id}/accept`, {
      baseRevision,
      forceRiskAccept: opts.forceRiskAccept === true,
    })
    note('已接受候选')
    return c
  }
  async function rejectCandidate(id: string, reason = '') { await api.post(`/api/candidates/${id}/reject`, { reason }) }

  return {
    books, currentBook, primary, continuity, candidates, candidateHistory, runs, activeRun,
    providers, models, presets, assignments, toast, error,
    booksLoading, booksError,
    filesLoadedRevision, historyLoaded, candidateDetails,
    note, fail, loadBooks, loadBook, loadBookFiles, filesReady,
    loadCandidates, loadCandidateHistory, loadRuns,
    candidateDetail, resolvedCandidate,
    loadLLM, startRun, pollRun, clearActiveRun, acceptCandidate, rejectCandidate,
  }
})
