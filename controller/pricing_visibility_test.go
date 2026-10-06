package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 模型广场相关的公开接口不得把访问者用不了的分组带出去。站点有只给管理员用的线路：
// 分组在倍率表里、有渠道，但不在 UserUsableGroups，挂的模型又与对客分组同名。不裁剪
// 的话，分组名会随同名模型的 enable_groups、性能数据出现在所有人的模型广场里。

const (
	visibilityPublicGroup = "zz-public"
	visibilityHiddenGroup = "zz-hidden"
	visibilitySharedModel = "zz-visibility-shared-model"
	visibilityHiddenModel = "zz-visibility-hidden-only-model"
	visibilityHiddenUser  = 2001
	visibilityPublicUser  = 2002
)

type pricingVisibilityResponse struct {
	Success     bool               `json:"success"`
	Data        []model.Pricing    `json:"data"`
	GroupRatio  map[string]float64 `json:"group_ratio"`
	UsableGroup map[string]string  `json:"usable_group"`
}

type perfMetricsVisibilityResponse struct {
	Success bool                    `json:"success"`
	Data    perfmetrics.QueryResult `json:"data"`
}

type perfSummaryVisibilityResponse struct {
	Success bool                         `json:"success"`
	Data    perfmetrics.SummaryAllResult `json:"data"`
}

func withVisibilityGroups(t *testing.T) {
	t.Helper()

	originalUsable := setting.UserUsableGroups2JSONString()
	originalRatio := ratio_setting.GroupRatio2JSONString()
	originalSpecial := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.ReadAll()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsable))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatio))
		special := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
		special.Clear()
		special.AddAll(originalSpecial)
		model.InvalidatePricingCache()
	})

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"zz-public":"对客分组"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"zz-public":1,"zz-hidden":1.5}`))
	ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Clear()
}

func seedVisibilityFixtures(t *testing.T) {
	t.Helper()

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}))
	require.NoError(t, db.Create(&[]model.User{
		{Id: visibilityHiddenUser, Username: "visibility-hidden-user", Password: "password",
			AffCode: "vis-hidden", Group: visibilityHiddenGroup, Status: common.UserStatusEnabled},
		{Id: visibilityPublicUser, Username: "visibility-public-user", Password: "password",
			AffCode: "vis-public", Group: visibilityPublicGroup, Status: common.UserStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: visibilityPublicGroup, Model: visibilitySharedModel, ChannelId: 1, Enabled: true},
		{Group: visibilityHiddenGroup, Model: visibilitySharedModel, ChannelId: 2, Enabled: true},
		{Group: visibilityHiddenGroup, Model: visibilityHiddenModel, ChannelId: 2, Enabled: true},
	}).Error)
	bucket := time.Now().Unix() - 600
	require.NoError(t, db.Create(&[]model.PerfMetric{
		{ModelName: visibilitySharedModel, Group: visibilityPublicGroup, BucketTs: bucket,
			RequestCount: 10, SuccessCount: 10, TotalLatencyMs: 1000},
		{ModelName: visibilitySharedModel, Group: visibilityHiddenGroup, BucketTs: bucket,
			RequestCount: 30, SuccessCount: 0, TotalLatencyMs: 90000},
		{ModelName: visibilityHiddenModel, Group: visibilityHiddenGroup, BucketTs: bucket,
			RequestCount: 5, SuccessCount: 5, TotalLatencyMs: 500},
	}).Error)
	model.InvalidatePricingCache()
}

// viewer 为 0 表示未登录。
func serveForViewer(t *testing.T, handler gin.HandlerFunc, target string, viewer int) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	if viewer != 0 {
		ctx.Set("id", viewer)
	}
	handler(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	return recorder
}

func decodePricingVisibility(t *testing.T, recorder *httptest.ResponseRecorder) pricingVisibilityResponse {
	t.Helper()

	var payload pricingVisibilityResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	return payload
}

func TestFilterPricingByUsableGroupsTrimsEnableGroups(t *testing.T) {
	pricing := []model.Pricing{
		{ModelName: "shared", EnableGroup: []string{"hidden", "public", "other"}},
		{ModelName: "hidden-only", EnableGroup: []string{"hidden"}},
		{ModelName: "public-only", EnableGroup: []string{"public"}},
	}

	cases := []struct {
		name   string
		usable map[string]string
		want   map[string][]string
	}{
		{
			name:   "访问者只有对客分组",
			usable: map[string]string{"public": "对客"},
			want:   map[string][]string{"shared": {"public"}, "public-only": {"public"}},
		},
		{
			name:   "访问者同时有隐藏分组",
			usable: map[string]string{"public": "对客", "hidden": "用户分组"},
			want: map[string][]string{
				"shared": {"hidden", "public"}, "hidden-only": {"hidden"}, "public-only": {"public"},
			},
		},
		{
			name:   "访问者的分组没有任何模型",
			usable: map[string]string{"nobody": "空"},
			want:   map[string][]string{},
		},
		{
			name:   "没有可用分组",
			usable: map[string]string{},
			want:   map[string][]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := map[string][]string{}
			for _, item := range filterPricingByUsableGroups(pricing, tc.usable) {
				got[item.ModelName] = item.EnableGroup
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

// 入参是 model.GetPricing() 的共享缓存，它的 EnableGroup 切片还被 modelEnableGroups
// 引用：原地裁剪会让下一个访问者（包括管理员）拿到被裁过的列表。
func TestFilterPricingByUsableGroupsLeavesInputUntouched(t *testing.T) {
	groups := []string{"hidden", "public"}
	pricing := []model.Pricing{{ModelName: "shared", EnableGroup: groups}}

	filtered := filterPricingByUsableGroups(pricing, map[string]string{"public": "对客"})

	require.Len(t, filtered, 1)
	assert.Equal(t, []string{"public"}, filtered[0].EnableGroup)
	assert.Equal(t, []string{"hidden", "public"}, groups)
	assert.Equal(t, []string{"hidden", "public"}, pricing[0].EnableGroup)
}

func TestGetPricingHidesGroupsTheViewerCannotUse(t *testing.T) {
	withVisibilityGroups(t)
	seedVisibilityFixtures(t)

	for _, viewer := range []int{0, visibilityPublicUser} {
		recorder := serveForViewer(t, GetPricing, "/api/pricing", viewer)
		payload := decodePricingVisibility(t, recorder)

		require.Len(t, payload.Data, 1, "viewer=%d", viewer)
		assert.Equal(t, visibilitySharedModel, payload.Data[0].ModelName)
		assert.Equal(t, []string{visibilityPublicGroup}, payload.Data[0].EnableGroup)
		assert.NotContains(t, payload.GroupRatio, visibilityHiddenGroup)
		assert.NotContains(t, payload.UsableGroup, visibilityHiddenGroup)
		assert.NotContains(t, recorder.Body.String(), visibilityHiddenGroup, "viewer=%d", viewer)
	}
}

func TestGetPricingShowsHiddenGroupToItsOwnUsers(t *testing.T) {
	withVisibilityGroups(t)
	seedVisibilityFixtures(t)

	payload := decodePricingVisibility(t,
		serveForViewer(t, GetPricing, "/api/pricing", visibilityHiddenUser))

	groupsByModel := map[string][]string{}
	for _, item := range payload.Data {
		groupsByModel[item.ModelName] = item.EnableGroup
	}
	require.Len(t, groupsByModel, 2)
	assert.ElementsMatch(t, []string{visibilityPublicGroup, visibilityHiddenGroup},
		groupsByModel[visibilitySharedModel])
	assert.Equal(t, []string{visibilityHiddenGroup}, groupsByModel[visibilityHiddenModel])
	assert.Equal(t, 1.5, payload.GroupRatio[visibilityHiddenGroup])
	assert.Contains(t, payload.UsableGroup, visibilityHiddenGroup)

	// 管理员看过之后，共享缓存不能被带偏：普通访问者仍然只看到对客分组。
	again := decodePricingVisibility(t, serveForViewer(t, GetPricing, "/api/pricing", 0))
	require.Len(t, again.Data, 1)
	assert.Equal(t, []string{visibilityPublicGroup}, again.Data[0].EnableGroup)
}

func TestGetPerfMetricsOnlyReturnsGroupsTheViewerCanUse(t *testing.T) {
	withVisibilityGroups(t)
	seedVisibilityFixtures(t)

	groupsFor := func(target string, viewer int) []string {
		recorder := serveForViewer(t, GetPerfMetrics, target, viewer)
		var payload perfMetricsVisibilityResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
		require.True(t, payload.Success)
		names := make([]string, 0, len(payload.Data.Groups))
		for _, group := range payload.Data.Groups {
			names = append(names, group.Group)
		}
		return names
	}
	target := "/api/perf-metrics?model=" + visibilitySharedModel

	assert.Equal(t, []string{visibilityPublicGroup}, groupsFor(target, 0))
	assert.Equal(t, []string{visibilityPublicGroup}, groupsFor(target, visibilityPublicUser))
	// 点名隐藏分组也拿不到。
	assert.Empty(t, groupsFor(target+"&group="+visibilityHiddenGroup, 0))
	assert.ElementsMatch(t, []string{visibilityPublicGroup, visibilityHiddenGroup},
		groupsFor(target, visibilityHiddenUser))
}

func TestGetPerfMetricsSummaryExcludesGroupsTheViewerCannotUse(t *testing.T) {
	withVisibilityGroups(t)
	seedVisibilityFixtures(t)

	summaryFor := func(viewer int) map[string]perfmetrics.ModelSummary {
		recorder := serveForViewer(t, GetPerfMetricsSummary, "/api/perf-metrics/summary", viewer)
		var payload perfSummaryVisibilityResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
		require.True(t, payload.Success)
		byName := map[string]perfmetrics.ModelSummary{}
		for _, item := range payload.Data.Models {
			byName[item.ModelName] = item
		}
		return byName
	}

	// 隐藏分组那 30 笔全失败的请求不计入对客看到的成功率，只挂在隐藏分组的模型不出现。
	// 按模型名断言而不数条数：内存里的热桶是进程级的，别的用例写过的模型也会在这里。
	public := summaryFor(0)
	require.Contains(t, public, visibilitySharedModel)
	assert.Equal(t, 100.0, public[visibilitySharedModel].SuccessRate)
	assert.NotContains(t, public, visibilityHiddenModel)

	hidden := summaryFor(visibilityHiddenUser)
	require.Contains(t, hidden, visibilitySharedModel)
	assert.Equal(t, 25.0, hidden[visibilitySharedModel].SuccessRate)
	assert.Contains(t, hidden, visibilityHiddenModel)
}

// 最近一个桶的数据还在内存里没落库（hotBuckets），查询把它和库里的行分两路合并、各自过滤。
func TestPerfMetricsHotBucketsRespectViewerGroups(t *testing.T) {
	withVisibilityGroups(t)
	seedVisibilityFixtures(t)

	const hotShared = "zz-visibility-hot-shared-model"
	const hotHidden = "zz-visibility-hot-hidden-only-model"
	perfmetrics.Record(perfmetrics.Sample{
		Model: hotShared, Group: visibilityPublicGroup, LatencyMs: 100, Success: true})
	for i := 0; i < 3; i++ {
		perfmetrics.Record(perfmetrics.Sample{
			Model: hotShared, Group: visibilityHiddenGroup, LatencyMs: 9000, Success: false})
	}
	perfmetrics.Record(perfmetrics.Sample{
		Model: hotHidden, Group: visibilityHiddenGroup, LatencyMs: 50, Success: true})

	summaryFor := func(viewer int) map[string]perfmetrics.ModelSummary {
		recorder := serveForViewer(t, GetPerfMetricsSummary, "/api/perf-metrics/summary", viewer)
		var payload perfSummaryVisibilityResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
		byName := map[string]perfmetrics.ModelSummary{}
		for _, item := range payload.Data.Models {
			byName[item.ModelName] = item
		}
		return byName
	}
	groupsFor := func(target string, viewer int) []string {
		recorder := serveForViewer(t, GetPerfMetrics, target, viewer)
		var payload perfMetricsVisibilityResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
		names := make([]string, 0, len(payload.Data.Groups))
		for _, group := range payload.Data.Groups {
			names = append(names, group.Group)
		}
		return names
	}

	public := summaryFor(0)
	require.Contains(t, public, hotShared)
	assert.Equal(t, 100.0, public[hotShared].SuccessRate)
	assert.NotContains(t, public, hotHidden)

	hidden := summaryFor(visibilityHiddenUser)
	require.Contains(t, hidden, hotShared)
	assert.Equal(t, 25.0, hidden[hotShared].SuccessRate)
	assert.Contains(t, hidden, hotHidden)

	target := "/api/perf-metrics?model=" + hotShared
	assert.Equal(t, []string{visibilityPublicGroup}, groupsFor(target, 0))
	assert.Empty(t, groupsFor(target+"&group="+visibilityHiddenGroup, 0))
	assert.Empty(t, groupsFor("/api/perf-metrics?model="+hotHidden, visibilityPublicUser))
	assert.ElementsMatch(t, []string{visibilityPublicGroup, visibilityHiddenGroup},
		groupsFor(target, visibilityHiddenUser))
}
