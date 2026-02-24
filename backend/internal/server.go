package internal

import (
	"context"
	"log/slog"
	"net"
	"net/http"

	"connectrpc.com/connect"
	"connectrpc.com/grpchealth"
	"github.com/go-chi/chi/v5"
	"github.com/rs/cors"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/kazz187/taskguild/backend/internal/agent"
	"github.com/kazz187/taskguild/backend/internal/agentmanager"
	"github.com/kazz187/taskguild/backend/internal/event"
	"github.com/kazz187/taskguild/backend/internal/interaction"
	"github.com/kazz187/taskguild/backend/internal/project"
	"github.com/kazz187/taskguild/backend/internal/pushnotification"
	"github.com/kazz187/taskguild/backend/internal/skill"
	"github.com/kazz187/taskguild/backend/internal/task"
	"github.com/kazz187/taskguild/backend/internal/workflow"
	"github.com/kazz187/taskguild/backend/internal/config"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	"github.com/kazz187/taskguild/backend/pkg/clog"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

type Server struct {
	server                 *http.Server
	env                    *config.Env
	projectServer          *project.Server
	workflowServer         *workflow.Server
	taskServer             *task.Server
	interactionServer      *interaction.Server
	agentManagerServer     *agentmanager.Server
	agentServer            *agent.Server
	skillServer            *skill.Server
	eventServer            *event.Server
	pushNotificationServer *pushnotification.Server
}

func NewServer(
	env *config.Env,
	projectServer *project.Server,
	workflowServer *workflow.Server,
	taskServer *task.Server,
	interactionServer *interaction.Server,
	agentManagerServer *agentmanager.Server,
	agentServer *agent.Server,
	skillServer *skill.Server,
	eventServer *event.Server,
	pushNotificationServer *pushnotification.Server,
) *Server {
	return &Server{
		env:                    env,
		projectServer:          projectServer,
		workflowServer:         workflowServer,
		taskServer:             taskServer,
		interactionServer:      interactionServer,
		agentManagerServer:     agentManagerServer,
		agentServer:            agentServer,
		skillServer:            skillServer,
		eventServer:            eventServer,
		pushNotificationServer: pushNotificationServer,
	}
}

// ListenAndServe starts the HTTP server. The provided context is used as the
// base context for all incoming requests via http.Server.BaseContext. When ctx
// is cancelled (e.g. on shutdown signal), all streaming RPC contexts are also
// cancelled, allowing the server to shut down without waiting for streams.
func (s *Server) ListenAndServe(ctx context.Context) error {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(
			clog.SlogChiMiddleware(),
			cerr.NewConvertConnectErrorChiMiddleware(),
		)
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			cerr.SetNewJSONError(r.Context(), cerr.NotFound, "not found", nil)
		})
	})

	mux := http.NewServeMux()

	mux.Handle("/health", &HealthChecker{})
	mux.Handle("/api/", r)
	mux.Handle(grpchealth.NewHandler(grpchealth.NewStaticChecker()))

	interceptors := s.interceptors()
	handlerOpts := connect.WithInterceptors(interceptors...)

	mux.Handle(taskguildv1connect.NewProjectServiceHandler(s.projectServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewWorkflowServiceHandler(s.workflowServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewTaskServiceHandler(s.taskServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewInteractionServiceHandler(s.interactionServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewAgentManagerServiceHandler(s.agentManagerServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewAgentServiceHandler(s.agentServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewSkillServiceHandler(s.skillServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewEventServiceHandler(s.eventServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewPushNotificationServiceHandler(s.pushNotificationServer, handlerOpts))

	addr := net.JoinHostPort(s.env.HTTPHost, s.env.HTTPPort)
	slog.Info("starting server", "addr", addr)

	s.server = &http.Server{
		Addr: addr,
		Handler: h2c.NewHandler(cors.New(cors.Options{
			AllowedOrigins:   []string{"*"},
			AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodOptions},
			AllowedHeaders:   []string{"*"},
			AllowCredentials: true,
		}).Handler(s.apiKeyMiddleware(mux)), &http2.Server{}),
		BaseContext: func(_ net.Listener) context.Context { return ctx },
	}

	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

type HealthChecker struct{}

func (hc *HealthChecker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) interceptors() []connect.Interceptor {
	return []connect.Interceptor{
		clog.NewSlogConnectInterceptor(),
		cerr.NewConvertConnectErrorInterceptor(),
	}
}

func (s *Server) apiKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip API key check for health endpoints.
		if r.URL.Path == "/health" || r.URL.Path == "/grpc.health.v1.Health/Check" {
			next.ServeHTTP(w, r)
			return
		}
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			apiKey = r.Header.Get("Authorization")
			if len(apiKey) > 7 && apiKey[:7] == "Bearer " {
				apiKey = apiKey[7:]
			}
		}
		if apiKey != s.env.APIKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
