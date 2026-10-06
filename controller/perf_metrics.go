package controller

import (
	"net/http"
	"strconv"

	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func GetPerfMetricsSummary(c *gin.Context) {
	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	usableGroup := service.GetUserUsableGroups(viewerGroup(c))
	result, err := perfmetrics.QuerySummaryAll(hours, visiblePerfGroups(usableGroup))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func GetPerfMetrics(c *gin.Context) {
	modelName := c.Query("model")
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "model is required",
		})
		return
	}

	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	result, err := perfmetrics.Query(perfmetrics.QueryParams{
		Model: modelName,
		Group: c.Query("group"),
		Hours: hours,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	result.Groups = filterVisibleGroups(result.Groups, service.GetUserUsableGroups(viewerGroup(c)))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// visiblePerfGroups 取「在倍率表里」且「访问者可用」的分组，外加 auto。只看倍率表会把
// 不对客的分组（如管理员专用线）的名字和数据带进公开的性能接口。
func visiblePerfGroups(usableGroup map[string]string) []string {
	groups := make([]string, 0, len(usableGroup)+1)
	for group := range ratio_setting.GetGroupRatioCopy() {
		if _, ok := usableGroup[group]; ok {
			groups = append(groups, group)
		}
	}
	return append(groups, "auto")
}

func filterVisibleGroups(groups []perfmetrics.GroupResult, usableGroup map[string]string) []perfmetrics.GroupResult {
	visible := lo.Keyify(visiblePerfGroups(usableGroup))
	return lo.Filter(groups, func(g perfmetrics.GroupResult, _ int) bool {
		_, ok := visible[g.Group]
		return ok
	})
}
