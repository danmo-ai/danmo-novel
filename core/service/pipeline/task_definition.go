package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	"github.com/danmo-ai/danmo-novel/core/resource/novel"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/prompt"
)

// OutputProtocol is registered with its parser, not merely quoted in a prompt.
type OutputProtocol = prompt.Contract

type Codec[T any] interface {
	Protocol() OutputProtocol
	Decode(raw string) (T, error)
}

type TaskInput struct {
	Snapshot domain.BookSnapshot
	Spec     domain.TaskSpec
}

type TaskDefinition[T any] struct {
	ID        string
	Version   int
	PromptKey string
	Build     func(TaskInput) (contextsvc.UserPayload, error)
	Codec     Codec[T]
	Validate  func(T, TaskInput) []Violation
	Budget    func(TaskInput, int) domain.CallPolicy
	Candidate func(T, TaskInput) domain.Candidate
}

type Invocation[T any] struct {
	TaskID         string
	StaticPrompt   string
	DynamicPayload contextsvc.UserPayload
	OutputContract OutputProtocol
	Policy         domain.CallPolicy
	ModelID        string
	Definition     TaskDefinition[T]
	Input          TaskInput
}

type TaskResult[T any] struct {
	Value     T
	Candidate domain.Candidate
	Calls     []domain.CallRecord
	Packet    domain.ContextPacket
}

// TaskRunner is the business-facing LLM surface. Providers remain a generic
// two-message transport and never learn novel-specific protocols.
type TaskRunner struct {
	LLM         port.LLMProvider
	ModelMaxOut func(string) int
	Knowledge   []string
}

func (r *TaskRunner) modelMax(id string) int {
	if r.ModelMaxOut == nil {
		return 0
	}
	return r.ModelMaxOut(id)
}

// ExecuteTask forces static instructions, dynamic data and output protocol to
// be supplied separately. Repair is appended only at the end of UserPayload.
func ExecuteTask[T any](ctx context.Context, runner *TaskRunner, call Invocation[T]) (TaskResult[T], error) {
	var zero TaskResult[T]
	def := call.Definition
	if def.Codec == nil || def.Candidate == nil {
		return zero, fmt.Errorf("任务 %s 未注册 codec/candidate", call.TaskID)
	}
	contract := def.Codec.Protocol()
	if call.TaskID != def.ID || call.OutputContract.ID != contract.ID || call.OutputContract.Hash != contract.Hash {
		return zero, fmt.Errorf("任务或输出协议未按注册表绑定")
	}
	if !strings.Contains(call.StaticPrompt, contract.Hash) || !strings.Contains(call.StaticPrompt, "输出协议 "+contract.ID) {
		return zero, fmt.Errorf("静态提示词未包含匹配的输出协议")
	}
	if call.ModelID == "" {
		return zero, fmt.Errorf("modelId required")
	}
	policy := call.Policy
	if def.Budget != nil {
		policy = def.Budget(call.Input, runner.modelMax(call.ModelID))
	}
	payload := call.DynamicPayload
	if payload.Task == "" {
		payload.Task = def.ID
	}
	blocks := make([]domain.ContextBlock, 0, len(payload.Documents))
	hashes := map[string]string{}
	for _, d := range payload.Documents {
		blocks = append(blocks, domain.ContextBlock{ID: d.ID, Label: d.Label, Source: d.Path, Content: d.Content, Bytes: len(d.Content)})
		hashes[d.ID+"|"+d.Path] = shortHash(d.Content)
	}
	packet := domain.ContextPacket{
		ContractVersion: fmt.Sprintf("%s@%d", contract.ID, contract.Version), TaskKind: call.Input.Spec.Kind,
		BookID: call.Input.Snapshot.Summary.ID, InputHashes: hashes, SystemPrompt: call.StaticPrompt,
		Policy: policy,
	}
	packet.UserPrompt = contextsvc.EncodeUserPayload(payload)
	for _, knowledge := range runner.Knowledge {
		packet.UserPrompt = contextsvc.MergeKnowledgeIntoUserPayload(packet.UserPrompt, []string{knowledge})
	}
	packet.Blocks = blocks
	packet.KnowledgeVersion = novel.KnowledgeVersion + "+" + shortHash(packet.UserPrompt)
	packet.EstPromptTokens = (len([]rune(packet.SystemPrompt)) + len([]rune(packet.UserPrompt))) / 2
	max := policy.MaxCalls
	if max < 1 {
		max = 1
	}
	var calls []domain.CallRecord
	var lastErr error
	var lastRaw string
	for attempt := 1; attempt <= max; attempt++ {
		if attempt > 1 && !policy.AllowOneRepair {
			break
		}
		user := packet.UserPrompt
		if attempt > 1 {
			user = contextsvc.MergeRepairIntoUserPayload(user, lastErr)
		}
		raw, rec, err := chatLLM(ctx, runner.LLM, call.ModelID, packet.SystemPrompt, user, policy)
		rec.Index = len(calls) + 1
		rec.Step = "generate"
		if attempt > 1 {
			rec.Step = "structure-repair"
		}
		calls = append(calls, rec)
		if err != nil {
			lastErr = err
			continue
		}
		lastRaw = raw
		value, err := def.Codec.Decode(raw)
		if err == nil && def.Validate != nil {
			if vs := def.Validate(value, call.Input); len(vs) > 0 {
				err = contractFail(def.ID, vs)
			}
		}
		if err == nil {
			candidate := def.Candidate(value, call.Input)
			candidate.Kind = call.Input.Spec.Kind
			candidate.BookID = call.Input.Snapshot.Summary.ID
			candidate.BaseRevision = call.Input.Snapshot.Revision
			packet.VolumeID = candidate.VolumeID
			packet.UnitID = candidate.UnitID
			return TaskResult[T]{Value: value, Candidate: candidate, Calls: calls, Packet: packet}, nil
		}
		lastErr = err
		// 截断只由响应协议判定；输出上限由 LLM client 按模型配置决定，
		// 上层不再持有可翻倍的预算。
		if truncatedOutput(rec) {
			lastErr = truncationRetryError(err)
		}
	}
	return TaskResult[T]{Calls: calls, Packet: packet}, fmt.Errorf("%s 未通过输出协议：%w；last=%s", def.ID, lastErr, clip(lastRaw, 200))
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
