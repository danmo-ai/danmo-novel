package main

import (
	"context"
	"log"
	"os"

	httpapi "github.com/danmo-ai/danmo-novel/core/adapter/http"
	llmadapt "github.com/danmo-ai/danmo-novel/core/adapter/llm"
	adaptermcp "github.com/danmo-ai/danmo-novel/core/adapter/mcp"
	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/runtimepaths"
	"github.com/danmo-ai/danmo-novel/core/service/chain"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
	"github.com/danmo-ai/danmo-novel/core/service/runs"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

func main() {
	settings, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := sqlite.Open(runtimepaths.DBPath())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	models := llmconfig.NewModelRegistry()
	models.SetModels(settings.Get().DefaultModels)
	llmMgr := llmconfig.NewManager(db, models)
	provider := llmadapt.NewDefaultLLMProvider(llmMgr, models)

	runSvc := runs.New(db, settings, provider)
	runSvc.Models = models
	chainSvc := chain.New(db, settings, runSvc)
	// 进程重启会丢掉内存里的 goroutine：把上次遗留的 running/pending 任务
	// 收尾，否则它们会永远停在运行中并挡住同单元的新任务。
	if n, err := runSvc.RecoverInterrupted(context.Background()); err != nil {
		log.Printf("warning: recover interrupted runs: %v", err)
	} else if n > 0 {
		log.Printf("recovered %d interrupted run(s) from previous process", n)
	}
	if n, err := chainSvc.RecoverInterrupted(context.Background()); err != nil {
		log.Printf("warning: recover interrupted chains: %v", err)
	} else if n > 0 {
		log.Printf("recovered %d interrupted chain(s) from previous process", n)
	}
	app := &httpapi.App{
		Settings: settings,
		Store:    db,
		LLMMgr:   llmMgr,
		Models:   models,
		LLM:      provider,
		Runs:     runSvc,
		Chains:   chainSvc,
	}
	r := httpapi.NewRouter(app)
	host := first(os.Getenv("DQ_HTTP_HOST"), "127.0.0.1")
	port := first(os.Getenv("DQ_BACKEND_PORT"), os.Getenv("DANMO_NOVEL_PORT"), "7804")
	addr := host + ":" + port
	if err := adaptermcp.WriteAPIPort(addr); err != nil {
		log.Printf("warning: write api.port: %v", err)
	}
	log.Printf("Danmo Novel API http://%s  mcp=http://%s/mcp/  home=%s", addr, addr, runtimepaths.Home())
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
