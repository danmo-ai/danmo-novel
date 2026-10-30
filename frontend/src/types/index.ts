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

export interface VolumeInfo { volume: number; fileName: string; cast: string[]; raw?: string }
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
  outlineRaw?: string
  proseRaw?: string
}
export interface CastCard { stem: string; name: string; tagline?: string; status: string; role: string; raw?: string }
export interface BookSnapshot {
  summary: BookSummary
  stateRaw?: string
  volumes: VolumeInfo[]
  units: UnitInfo[]
  cast: CastCard[]
  files: Record<string, string>
  revision: string
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
  chapter?: number
  suggestion?: string
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
  unitOutlines?: UnitOutline[]
  mode?: string
  planMarkdown?: Record<string, string>
  planSettings?: Record<string, string>
  replacement?: string
  findings?: ReviewFinding[]
  canonChanges?: any[]
  createdAt: string
}
export interface CallRecord {
  index: number
  modelId: string
  provider: string
  step: string
  promptTokens: number
  completionTokens: number
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
