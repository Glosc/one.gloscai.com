package sora

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseTaskResultExtractsAgnesVideoURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	taskInfo, err := adaptor.ParseTaskResult(nil, nil, []byte(`{
		"id": "task_123",
		"video_id": "video_123",
		"model": "agnes-video-v2.0",
		"status": "completed",
		"progress": 100,
		"remixed_from_video_id": "https://storage.example.com/result.mp4"
	}`))

	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, taskInfo.Status)
	require.Equal(t, "https://storage.example.com/result.mp4", taskInfo.Url)
}

func TestFetchTaskUsesAgnesVideoIDEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/agnesapi", r.URL.Path)
		require.Equal(t, "video_123", r.URL.Query().Get("video_id"))
		require.Equal(t, "agnes-video-v2.0", r.URL.Query().Get("model_name"))
		_, _ = w.Write([]byte(`{"status":"completed"}`))
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{}
	resp, err := adaptor.FetchTask(server.URL, "test-key", &model.Task{TaskID: "task_123", Properties: model.Properties{OriginModelName: "agnes-video-v2.0"}, Data: []byte(`{"video_id":"video_123"}`)}, "")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"status":"completed"}`, string(body))
}

func TestConvertToOpenAIVideoAddsPlayableMetadataURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID: "task_public",
		Data:   []byte(`{"id":"upstream_task","status":"completed","metadata":{}}`),
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://cdn.example.com/result.mp4",
		},
	}

	data, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(data, &payload))
	require.Equal(t, "task_public", payload["id"])
	require.Equal(t, "task_public", payload["task_id"])
	metadata, ok := payload["metadata"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://cdn.example.com/result.mp4", metadata["url"])
}

func TestBuildRequestBodyConvertsMultipartWithoutFilesToJSON(t *testing.T) {
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	require.NoError(t, writer.WriteField("model", "Agnes/agnes-video-v2.0"))
	require.NoError(t, writer.WriteField("prompt", "p"))
	require.NoError(t, writer.WriteField("duration", "5"))
	require.NoError(t, writer.WriteField("width", "1024"))
	require.NoError(t, writer.WriteField("height", "576"))
	require.NoError(t, writer.WriteField("fps", "24"))
	require.NoError(t, writer.WriteField("n", "1"))
	require.NoError(t, writer.WriteField("metadata", `{"quality_level":"standard"}`))
	require.NoError(t, writer.Close())

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &form)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "upstream-video"},
	}
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)

	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "application/json", c.Request.Header.Get("Content-Type"))

	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(bodyBytes, &payload))
	require.Equal(t, "upstream-video", payload["model"])
	require.Equal(t, "p", payload["prompt"])
	require.Equal(t, float64(5), payload["duration"])
	require.Equal(t, float64(1024), payload["width"])
	require.Equal(t, float64(576), payload["height"])
	require.Equal(t, float64(24), payload["fps"])
	require.Equal(t, float64(1), payload["n"])
	metadata, ok := payload["metadata"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "standard", metadata["quality_level"])
}
