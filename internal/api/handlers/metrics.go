package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/troikatech/calling-agent/pkg/metrics"
)

func (h *Handler) GetMetrics(c *gin.Context) {
	metricsData := metrics.GetMetrics()
	c.JSON(http.StatusOK, metricsData)
}

func (h *Handler) GetPrometheusMetrics(c *gin.Context) {
	promMetrics := metrics.GetPrometheusMetrics()
	c.Data(http.StatusOK, "text/plain; version=0.0.4", []byte(promMetrics))
}

