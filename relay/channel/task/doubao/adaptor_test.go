package doubao

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoubaoTaskAPIBaseURL(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		channelType int
		want        string
	}{
		{
			name:        "empty uses doubao default api base",
			baseURL:     "",
			channelType: constant.ChannelTypeDoubaoVideo,
			want:        constant.ChannelBaseURLs[constant.ChannelTypeDoubaoVideo] + "/api/v3",
		},
		{
			name:        "empty agent plan uses plan api base",
			baseURL:     "",
			channelType: constant.ChannelTypeVolcEnginePlan,
			want:        constant.ChannelBaseURLs[constant.ChannelTypeVolcEnginePlan],
		},
		{
			name:        "root base uses normal ark api",
			baseURL:     "https://ark.cn-beijing.volces.com/",
			channelType: constant.ChannelTypeDoubaoVideo,
			want:        "https://ark.cn-beijing.volces.com/api/v3",
		},
		{
			name:        "normal ark api base is preserved",
			baseURL:     "https://ark.cn-beijing.volces.com/api/v3/",
			channelType: constant.ChannelTypeVolcEngine,
			want:        "https://ark.cn-beijing.volces.com/api/v3",
		},
		{
			name:        "agent plan api base is preserved",
			baseURL:     "https://ark.cn-beijing.volces.com/api/plan/v3/",
			channelType: constant.ChannelTypeVolcEnginePlan,
			want:        "https://ark.cn-beijing.volces.com/api/plan/v3",
		},
		{
			name:        "agent plan replaces normal ark api suffix",
			baseURL:     "https://ark.cn-beijing.volces.com/api/v3/",
			channelType: constant.ChannelTypeVolcEnginePlan,
			want:        "https://ark.cn-beijing.volces.com/api/plan/v3",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, doubaoTaskAPIBaseURL(test.baseURL, test.channelType))
		})
	}
}

func TestAgentPlanVideoTaskEndpoints(t *testing.T) {
	service.InitHttpClient()

	type recordedRequest struct {
		method        string
		path          string
		authorization string
		query         url.Values
		body          string
	}
	requests := make([]recordedRequest, 0, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requests = append(requests, recordedRequest{
			method:        r.Method,
			path:          r.URL.Path,
			authorization: r.Header.Get("Authorization"),
			query:         r.URL.Query(),
			body:          string(body),
		})
		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write([]byte(`{"id":"task-upstream"}`))
		require.NoError(t, err)
	}))
	defer upstream.Close()

	credential := `{"api_key":"agent-plan-key","access_key":"ak","secret_key":"sk"}`
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeVolcEnginePlan,
			ChannelBaseUrl: upstream.URL,
			ApiKey:         credential,
		},
	}
	adaptor.Init(info)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	resp, err := adaptor.DoRequest(c, info, bytes.NewBufferString(`{"model":"doubao-seedance-2.0"}`))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())

	resp, err = adaptor.FetchTask(upstream.URL, credential, &model.Task{TaskID: "task-upstream"}, "")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	resp, err = adaptor.FetchTaskList(upstream.URL, credential, channel.TaskListParams{
		PageNum:     2,
		PageSize:    20,
		Status:      "running",
		TaskIDs:     []string{"task-a", "task-b"},
		Model:       "doubao-seedance-2.0",
		ServiceTier: "default",
	}, "")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	resp, err = adaptor.DeleteTask(upstream.URL, credential, "task-upstream", "")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.Len(t, requests, 4)
	for _, request := range requests {
		assert.Equal(t, "Bearer agent-plan-key", request.authorization)
	}
	assert.Equal(t, http.MethodPost, requests[0].method)
	assert.Equal(t, "/api/plan/v3/contents/generations/tasks", requests[0].path)
	assert.JSONEq(t, `{"model":"doubao-seedance-2.0"}`, requests[0].body)

	assert.Equal(t, http.MethodGet, requests[1].method)
	assert.Equal(t, "/api/plan/v3/contents/generations/tasks/task-upstream", requests[1].path)

	assert.Equal(t, http.MethodGet, requests[2].method)
	assert.Equal(t, "/api/plan/v3/contents/generations/tasks", requests[2].path)
	assert.Equal(t, "2", requests[2].query.Get("page_num"))
	assert.Equal(t, "20", requests[2].query.Get("page_size"))
	assert.Equal(t, "running", requests[2].query.Get("filter.status"))
	assert.Equal(t, []string{"task-a", "task-b"}, requests[2].query["filter.task_ids"])
	assert.Equal(t, "doubao-seedance-2.0", requests[2].query.Get("filter.model"))
	assert.Equal(t, "default", requests[2].query.Get("filter.service_tier"))

	assert.Equal(t, http.MethodDelete, requests[3].method)
	assert.Equal(t, "/api/plan/v3/contents/generations/tasks/task-upstream", requests[3].path)
}

func TestParseAgentPlanCancelledVideoTask(t *testing.T) {
	adaptor := &TaskAdaptor{}

	result, err := adaptor.ParseTaskResult(nil, nil, []byte(`{"id":"task-upstream","status":"cancelled"}`))

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, string(model.TaskStatusFailure), result.Status)
	assert.Equal(t, "100%", result.Progress)
	assert.Equal(t, "cancelled", result.Reason)
}

func TestConvertToRequestPayloadRejectsOversizedMetadataDuration(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := &relaycommon.TaskSubmitReq{
		Model:  "doubao-seedance-2.0",
		Prompt: "generate a video",
		Metadata: map[string]any{
			"duration": relaycommon.MaxTaskDurationSeconds + 1,
		},
	}

	payload, err := adaptor.convertToRequestPayload(req)

	require.Error(t, err)
	assert.Nil(t, payload)
	assert.EqualError(t, err, "duration must be between 1 and 3600")
}
