package httpapi

import (
	"net/http"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/gin-gonic/gin"
)

// registerChains exposes 连续单元重写 (chain) control. A chain is only an
// orchestrator: every stage still produces a normal run + candidate, so the
// model log and manual repair paths are unchanged.
func registerChains(api *gin.RouterGroup, app *App) {
	api.POST("/chains", func(c *gin.Context) {
		var spec domain.ChainSpec
		if err := c.BindJSON(&spec); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if app.Chains == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chain service unavailable"})
			return
		}
		ch, err := app.Chains.Start(c.Request.Context(), spec)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "chain": ch})
			return
		}
		c.JSON(http.StatusOK, ch)
	})
	api.GET("/chains", func(c *gin.Context) {
		if app.Chains == nil {
			c.JSON(http.StatusOK, []domain.Chain{})
			return
		}
		list, err := app.Chains.List(c.Request.Context(), c.Query("bookId"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, list)
	})
	api.GET("/chains/:id", func(c *gin.Context) {
		if app.Chains == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chain service unavailable"})
			return
		}
		ch, err := app.Chains.Get(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(404, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, ch)
	})
	api.POST("/chains/:id/resume", func(c *gin.Context) {
		if app.Chains == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chain service unavailable"})
			return
		}
		ch, err := app.Chains.Resume(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error(), "chain": ch})
			return
		}
		c.JSON(200, ch)
	})
	api.POST("/chains/:id/cancel", func(c *gin.Context) {
		if app.Chains == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "chain service unavailable"})
			return
		}
		if err := app.Chains.Cancel(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	api.GET("/books/:id/chains", func(c *gin.Context) {
		if app.Chains == nil {
			c.JSON(http.StatusOK, []domain.Chain{})
			return
		}
		list, err := app.Chains.List(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, list)
	})
}
