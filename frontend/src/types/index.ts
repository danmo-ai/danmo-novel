export interface BookSummary {
  id: string
  title: string
  genre: string
  subgenre: string
  stage: string
  lastCommittedCh: number
  nextAction: string
  timeSystem?: string
  unitCount: number
  draftedCount: number
  reviewedCount: number
  openCandidates: number
  rootPath: string
  readOnly: boolean
}

export interface MarkdownField { name: string; value: string; range: { line: number; column: number; endLine: number; endColumn: number; byteOffset: number; byteEnd: number } }
export interface MarkdownEntity { kind: string; id: string; fields: MarkdownField[]; range: MarkdownField['range'] }
export interface StructuredMarkdown {
  protocol: string
  version: number
  title: string
  root: MarkdownEntity
  entities: MarkdownEntity[]
  diagnostics?: { path: string; message: string; severity: string; line?: number }[]
  legacy: boolean
}
export interface VolumeInfo { volume: number; fileName: string; cast: string[]; raw?: string; structured?: StructuredMarkdown }
export interface UnitInfo {
  unitId: string
  volume: number
  index: number
  status: string
  title: string
  chapterFrom: number
  chapterTo: number
  hasProse: boolean
  outlinePath: string
  prosePath: string
  /** 派生出的单章目标字数（后端 books.ChapterTarget），UI 不要自己算。 */
  chapterTarget: number
  outlineRaw?: string
  proseRaw?: string
}
export interface CastCard { stem: string; name: string; tagline?: string; status: string; role: string; raw?: string }
export interface IdentitySegment { stem: string; roleTitle: string; org?: string; fromUnit: string; toUnit?: string; note?: string }
export interface BookSnapshot {
  summary: BookSummary
  stateRaw?: string
  volumes: VolumeInfo[]
  units: UnitInfo[]
  cast: CastCard[]
  files: Record<string, string>
  revision: string
  structuredOutlines?: Record<string, StructuredMarkdown>
  identitySegments?: IdentitySegment[]
}

export interface ChapterDraft { chapter: number; title: string; body: string }
export interface ContextBlock {
  id: string
  label: string
  source: string
  content: string
  bytes: number
}
export interface ContextPacket {
  contractVersion: string
  taskKind: string
  bookId: string
  unitId?: string
  volumeId?: string
  blocks: ContextBlock[]
  knowledgeVersion: string
  estPromptTokens: number
  policy: { maxCalls: number; maxOutputTokens: number; timeoutSec: number; allowOneRepair: boolean }
  systemPrompt: string
  userPrompt: string
}
export interface ReviewFinding {
  id: string
  severity: string
  source: string
  lens?: string
  title: string
  evidence?: string
  /** 问题所在处的逐字原文，用于跨轮去重与定位。 */
  anchor?: string
  /**
   * 被多少轮独立审校检出（1 = 只有一轮看到）。
   * 实测单轮只覆盖约 31% 的问题，所以命中多次的更可能是可复现的硬伤；
   * 只有 roundsHit >= 2 的 blocking 才会阻断通过。
   */
  roundsHit?: number
  chapter?: number
  suggestion?: string
  entityPath?: string
  volumeId?: string
  unitId?: string
}
export interface UnitOutline {
  unitId?: string
  title?: string
  [key: string]: any
}

export interface Candidate {
  id: string
  runId: string
  bookId: string
  kind: string
  status: string
  baseRevision: string
  targetPath?: string
  unitId?: string
  volumeId?: string
  chapters?: ChapterDraft[]
  /** 修订候选的修订前正文；用于 diff 后再确认落盘。 */
  baseChapters?: ChapterDraft[]
  unitOutlines?: UnitOutline[]
  mode?: string
  /** reinit 接受时保留的既有路径（不覆盖）。 */
  preservePaths?: string[]
  planMarkdown?: Record<string, string>
  planSettings?: Record<string, string>
  replacement?: string
  findings?: ReviewFinding[]
  actualChapters?: { chapter: number; summary: string; stateDeltas?: { stem: string; field: string; from: string; to: string; evidence: string; confirmed: boolean }[]; plant?: string[]; payoff?: string[] }[]
  canonChanges?: any[]
  createdAt: string
  acceptedAt?: string
}

export interface CallRecord {
  index: number
  modelId: string
  provider: string
  step: string
  promptTokens: number
  completionTokens: number
  /**
   * completionTokens 里属于思维链的部分（已包含在内，不要重复相加）。
   * 审校这类"答案很短、思考很长"的调用，靠这两个数才能真正看清成本。
   */
  reasoningTokens?: number
  /** 命中 provider 前缀缓存的 prompt token 数（重试的修复调用应接近全量）。 */
  cacheReadTokens?: number
  cacheCreationTokens?: number
  durationMs: number
  error?: string
  startedAt: string
}
export interface Run {
  id: string
  bookId: string
  kind: string
  status: string
  spec: any
  policy: { maxCalls: number; maxOutputTokens: number; timeoutSec: number; allowOneRepair: boolean }
  modelId: string
  callsUsed: number
  calls: CallRecord[]
  candidateId?: string
  contextSummary?: string[]
  error?: string
  createdAt: string
}
export interface ProviderPublic {
  id: string
  provider: string
  name: string
  baseUrl?: string
  defaultProtocolDialect?: string
  hasApiKey: boolean
  models: { name: string; enabled: boolean; protocolDialect?: string }[]
  createdAt: string
  updatedAt: string
}
export interface LLMModel {
  id: string
  name: string
  providerId: string
  provider: string
  enabled: boolean
  availableEfforts?: string[]
  protocolDialect?: string
}

export interface ModelConfig {
  model: string
  context_window?: number
  max_output?: number
  temperature?: number
  top_p?: number
  frequency_penalty?: number
  presence_penalty?: number
  stop?: string[]
  available_efforts?: string[]
  vision?: boolean
}
