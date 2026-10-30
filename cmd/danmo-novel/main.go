package main

import (
	"log"
	"os"

	llmadapt "github.com/danmo-ai/danmo-novel/core/adapter/llm"
	httpapi "github.com/danmo-ai/danmo-novel/core/adapter/http"
	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/runtimepaths"
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
	app := &httpapi.App{
		Settings: settings,
		Store:    db,
		LLMMgr:   llmMgr,
		Models:   models,
		LLM:      provider,
		Runs:     runSvc,
	}
	r := httpapi.NewRouter(app)
	host := first(os.Getenv("DQ_HTTP_HOST"), "127.0.0.1")
	port := first(os.Getenv("DQ_BACKEND_PORT"), os.Getenv("DANMO_NOVEL_PORT"), "7804")
	addr := host + ":" + port
	log.Printf("Danmo Novel API http://%s  home=%s", addr, runtimepaths.Home())
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
