package llm

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/danmo-ai/danmo-novel/core/port"
)

var _ port.LLMProvider = (*MockClient)(nil)

type callStep struct {
	Text  string
	Delay time.Duration
}

// MockClient is an in-process LLM used by tests and demos.
type MockClient struct {
	mu       sync.Mutex
	steps    []callStep
	cursor   int
	Requests []port.LLMChatRequest
}

// MockProvider is a compatibility alias for MockClient.
type MockProvider = MockClient

func NewMock() *MockClient { return &MockClient{} }

func (p *MockClient) AddText(content string) *MockClient {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps = append(p.steps, callStep{Text: content})
	return p
}

// AddTextWithDelay queues a text step that waits for d (or context
// cancellation) before returning. Useful to keep a turn "in flight" so tests
// can cancel it deterministically.
func (p *MockClient) AddTextWithDelay(content string, d time.Duration) *MockClient {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps = append(p.steps, callStep{Text: content, Delay: d})
	return p
}

func (p *MockClient) Chat(ctx context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	p.mu.Lock()

	p.Requests = append(p.Requests, req)

	if len(req.Messages) > 0 && req.Messages[0].Role == "system" &&
		strings.Contains(req.Messages[0].Content, "You are a session title generator") {
		p.mu.Unlock()
		return port.LLMChatResponse{Content: "Generated Title", Done: true}, nil
	}

	if p.cursor >= len(p.steps) {
		// Optional canned response for demos / E2E without external models.
		if canned := os.Getenv("NOVEL_MOCK_RESPONSE"); canned != "" {
			p.mu.Unlock()
			return port.LLMChatResponse{Content: canned, Done: true}, nil
		}
		// Empty body is valid JSON parse failure upstream; tests always queue steps.
		p.mu.Unlock()
		return port.LLMChatResponse{Content: "done", Done: true}, nil
	}
	step := p.steps[p.cursor]
	p.cursor++
	p.mu.Unlock()

	if step.Delay > 0 {
		// Sleep outside the lock so concurrent Chat calls (e.g. title
		// generation) are not blocked by a delayed step.
		select {
		case <-time.After(step.Delay):
		case <-ctx.Done():
			return port.LLMChatResponse{}, ctx.Err()
		}
	}

	return port.LLMChatResponse{Content: step.Text, Done: true}, nil
}
