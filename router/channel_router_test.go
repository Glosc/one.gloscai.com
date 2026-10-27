package router

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelDefaultBaseURLsRequireReadPermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodGet, "/default_base_urls", authz.ChannelRead, controller.GetChannelDefaultBaseURLs)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerChannelRoutes(engine.Group("/api"))
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/channel/default_base_urls", nil))
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestChannelStatusRoutesUseExpectedPermissions(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodGet, "/priority/order", authz.ChannelRead, controller.GetChannelPriorityOrder)
	assertChannelRoutePermission(t, http.MethodPost, "/priority/reorder", authz.ChannelWrite, controller.ReorderChannelPriority)
	assertChannelRoutePermission(t, http.MethodGet, "/:id/vllm/status", authz.ChannelRead, controller.GetVLLMChannelStatus)
	assertChannelRoutePermission(t, http.MethodGet, "/:id/sglang/status", authz.ChannelRead, controller.GetSGLangChannelStatus)
	assertChannelRoutePermission(t, http.MethodPost, "/:id/status", authz.ChannelOperate, controller.UpdateChannelStatus)
	assertChannelRoutePermission(t, http.MethodPost, "/status/batch", authz.ChannelOperate, controller.BatchUpdateChannelStatus)
	assertChannelRoutePermission(t, http.MethodPut, "/", authz.ChannelWrite, controller.UpdateChannel)
}

func TestChannelDeleteRoutesUseSensitiveWritePermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodDelete, "/:id", authz.ChannelSensitiveWrite, controller.DeleteChannel)
	assertChannelRoutePermission(t, http.MethodPost, "/batch", authz.ChannelSensitiveWrite, controller.DeleteChannelBatch)
	assertChannelRoutePermission(t, http.MethodDelete, "/disabled", authz.ChannelSensitiveWrite, controller.DeleteDisabledChannel)
	assertChannelRoutePermission(t, http.MethodPut, "/", authz.ChannelWrite, controller.UpdateChannel)
	assertChannelRoutePermission(t, http.MethodPut, "/tag", authz.ChannelWrite, controller.EditTagChannels)
	assertChannelRoutePermission(t, http.MethodPost, "/batch/tag", authz.ChannelWrite, controller.BatchSetChannelTag)
	assertChannelRoutePermission(t, http.MethodDelete, "/:id/agent-plan/video/tasks/:task_id", authz.ChannelSensitiveWrite, controller.DeleteAgentPlanVideoTask)
}

func TestAgentPlanVideoTaskRoutesUseChannelPermissions(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodGet, "/:id/agent-plan/video/tasks", authz.ChannelOperate, controller.ListAgentPlanVideoTasks)
	assertChannelRoutePermission(t, http.MethodGet, "/:id/agent-plan/video/tasks/:task_id", authz.ChannelOperate, controller.GetAgentPlanVideoTask)
}

func TestChannelOAuthRoutesUseSensitiveWritePermission(t *testing.T) {
	for _, route := range []struct {
		path    string
		handler any
	}{
		{"/codex/oauth/start", controller.StartCodexOAuth},
		{"/codex/oauth/complete", controller.CompleteCodexOAuth},
		{"/:id/codex/oauth/start", controller.StartCodexOAuthForChannel},
		{"/:id/codex/oauth/complete", controller.CompleteCodexOAuthForChannel},
		{"/github-copilot/oauth/start", controller.StartGitHubCopilotOAuth},
		{"/github-copilot/oauth/complete", controller.CompleteGitHubCopilotOAuth},
		{"/:id/github-copilot/oauth/start", controller.StartGitHubCopilotOAuthForChannel},
		{"/:id/github-copilot/oauth/complete", controller.CompleteGitHubCopilotOAuthForChannel},
	} {
		assertChannelRoutePermission(t, http.MethodPost, route.path, authz.ChannelSensitiveWrite, route.handler)
	}
}

func TestChannelStatusRoutesRegisterWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api")

	require.NotPanics(t, func() {
		registerChannelRoutes(api)
	})
}

func TestApiRouterRegistersChannelStatusRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	require.NotPanics(t, func() {
		SetApiRouter(engine)
	})

	assertApiRouteRegistered(t, engine, http.MethodPost, "/api/channel/:id/status")
	assertApiRouteRegistered(t, engine, http.MethodPost, "/api/channel/status/batch")
}

func assertChannelRoutePermission(t *testing.T, method string, path string, permission authz.Permission, handler any) {
	t.Helper()
	for _, route := range channelPermissionRoutes {
		if route.method == method && route.path == path {
			assert.Equal(t, permission, route.permission)
			assert.Equal(t, reflect.ValueOf(handler).Pointer(), reflect.ValueOf(route.handler).Pointer())
			return
		}
	}
	t.Fatalf("route %s %s not found", method, path)
}

func assertApiRouteRegistered(t *testing.T, engine *gin.Engine, method string, path string) {
	t.Helper()
	for _, route := range engine.Routes() {
		if route.Method == method && route.Path == path {
			return
		}
	}
	t.Fatalf("route %s %s not found", method, path)
}
