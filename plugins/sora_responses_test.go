package plugins_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/relay/channel"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSoraResponsesProtocol(t *testing.T) {
	testVideoResponsesProtocol(t, videoResponsesTestCase{
		pluginKey: "sora",
		model:     "sora-2-pro",
		requestBody: map[string]any{
			"model":   "sora-2-pro",
			"input":   "waves at sunset",
			"seconds": 8,
			"size":    "1792x1024",
		},
		wantAction: "text_to_video",
		wantRequest: map[string]any{
			"model":   "sora-2-pro",
			"prompt":  "waves at sunset",
			"seconds": float64(8),
			"size":    "1792x1024",
		},
		wantUsageKeys:  []string{"seconds", "size"},
		wantVendorName: "sora",
	})
}

func TestSoraRetainsLegacyPricesAndVideoLocations(t *testing.T) {
	source, err := builtinplugins.Source("sora")
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "sora"})
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{OriginModelName: "sora-2-pro", TaskRelayInfo: &relaycommon.TaskRelayInfo{}, ChannelMeta: &relaycommon.ChannelMeta{
		ChannelType: constant.ChannelTypeOpenAI, ChannelBaseUrl: "https://api.example.com", UpstreamModelName: "sora-2-pro", ApiKey: "private-key",
	}}
	adaptor := taskplugin.New(plugin)
	adaptor.Init(info)
	for _, size := range []string{"720x1280", "1792x1024", "1024x1792"} {
		t.Run(size, func(t *testing.T) {
			for _, purpose := range []string{"facts", "billing_ratios"} {
				value, callErr := plugin.Engine.Call(t.Context(), "extractUsage", map[string]any{
					"usagePurpose": purpose, "requestBody": map[string]any{"seconds": 6, "size": size},
				})
				require.NoError(t, callErr)
				facts := decodePluginValue(t, value)
				assert.Equal(t, float64(6), facts["seconds"])
				assert.Equal(t, size, facts["size"])
				if purpose == "facts" {
					assert.NotContains(t, facts, "size_ratio")
					continue
				}
				ratio := 1.666667
				if size == "720x1280" {
					ratio = 1
				}
				assert.Equal(t, ratio, facts["size_ratio"])
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
			ctx.Set("task_request", map[string]any{"seconds": 6, "size": size})
			ratios, err := adaptor.EstimateBillingValidated(ctx, info)
			require.NoError(t, err)
			assert.Equal(t, float64(6), ratios["seconds"])
			wantRatio := 1.666667
			if size == "720x1280" {
				wantRatio = 1
			}
			assert.Equal(t, wantRatio, ratios["size_ratio"])
			assert.NotContains(t, ratios, "size", "the enum stays available to expressions without becoming a second multiplier")
		})
	}
	for _, tc := range []struct {
		name string
		body map[string]any
		url  string
	}{
		{"direct URL", map[string]any{"url": "https://cdn.example.com/direct.mp4"}, "https://cdn.example.com/direct.mp4"},
		{"URL priority", map[string]any{"url": "https://cdn.example.com/first.mp4", "video_url": "https://cdn.example.com/second.mp4"}, "https://cdn.example.com/first.mp4"},
		{"skip unsafe URL", map[string]any{"url": "javascript:alert(1)", "video_url": "https://cdn.example.com/safe.mp4"}, "https://cdn.example.com/safe.mp4"},
		{"video URL", map[string]any{"video_url": "https://cdn.example.com/video.mp4"}, "https://cdn.example.com/video.mp4"},
		{"result URL", map[string]any{"result_url": "https://cdn.example.com/result.mp4"}, "https://cdn.example.com/result.mp4"},
		{"output", map[string]any{"output": "https://cdn.example.com/output.mp4"}, "https://cdn.example.com/output.mp4"},
		{"content", map[string]any{"content": map[string]any{"video_url": "https://cdn.example.com/content.mp4"}}, "https://cdn.example.com/content.mp4"},
		{"metadata", map[string]any{"metadata": map[string]any{"url": "https://cdn.example.com/metadata.mp4"}}, "https://cdn.example.com/metadata.mp4"},
		{"legacy remixed URL", map[string]any{"remixed_from_video_id": "https://cdn.example.com/remixed.mp4"}, "https://cdn.example.com/remixed.mp4"},
		{"opaque remix ID", map[string]any{"remixed_from_video_id": "video_123"}, ""},
		{"unsupported scheme", map[string]any{"url": "javascript:alert(1)"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.body["status"] = "completed"
			value, callErr := plugin.Engine.Call(t.Context(), "parseTaskResult", map[string]any{}, tc.body)
			require.NoError(t, callErr)
			result := decodePluginValue(t, value)
			assert.Equal(t, "SUCCESS", result["status"])
			if tc.url == "" {
				assert.NotContains(t, result, "url")
			} else {
				assert.Equal(t, tc.url, result["url"])
			}
			value, callErr = plugin.Engine.Call(t.Context(), "buildContentRequest", map[string]any{
				"artifactKey": "video", "data": tc.body, "baseUrl": "https://api.example.com", "apiKey": "private-key", "upstreamTaskId": "video_123", "clientRequest": map[string]any{"method": "GET"},
			})
			require.NoError(t, callErr)
			content := decodePluginValue(t, value)
			if tc.url == "" {
				assert.Equal(t, "https://api.example.com/v1/videos/video_123/content", content["url"])
				return
			}
			assert.Equal(t, tc.url, content["url"])
			assert.Equal(t, true, content["credentialless"])
			assert.NotContains(t, content, "headers")
			data, err := common.Marshal(tc.body)
			require.NoError(t, err)
			descriptor, err := adaptor.BuildContentRequest(t.Context(), &model.Task{TaskID: "task_public", Data: data}, "video", channel.TaskArtifactClientRequest{Method: http.MethodGet})
			require.NoError(t, err)
			assert.True(t, descriptor.Credentialless)
			assert.Equal(t, tc.url, descriptor.URL)
			assert.Empty(t, descriptor.Headers, "the upstream API credential must not be sent to the video CDN")
			value, callErr = plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_video", "render"}, map[string]any{}, map[string]any{"task_id": "task_public", "status": "SUCCESS", "data": tc.body})
			require.NoError(t, callErr)
			assert.Equal(t, map[string]any{"url": tc.url}, decodePluginValue(t, value)["metadata"])
			if tc.name != "metadata" {
				assert.NotContains(t, tc.body, "metadata", "rendering must not mutate a hook argument")
			}
		})
	}
}
