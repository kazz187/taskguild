package internal

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/grpchealth"
	"github.com/go-chi/chi/v5"
	"github.com/rs/cors"

	"github.com/kazz187/taskguild/internal/agent"
	"github.com/kazz187/taskguild/internal/agentmanager"
	"github.com/kazz187/taskguild/internal/claudesettings"
	"github.com/kazz187/taskguild/internal/config"
	"github.com/kazz187/taskguild/internal/event"
	"github.com/kazz187/taskguild/internal/interaction"
	"github.com/kazz187/taskguild/internal/permission"
	"github.com/kazz187/taskguild/internal/project"
	"github.com/kazz187/taskguild/internal/pushnotification"
	"github.com/kazz187/taskguild/internal/schedule"
	"github.com/kazz187/taskguild/internal/script"
	"github.com/kazz187/taskguild/internal/singlecommandpermission"
	"github.com/kazz187/taskguild/internal/skill"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/internal/tasklog"
	tmpl "github.com/kazz187/taskguild/internal/template"
	"github.com/kazz187/taskguild/internal/workflow"
	"github.com/kazz187/taskguild/pkg/cerr"
	"github.com/kazz187/taskguild/pkg/clog"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

const (
	// maxConcurrentStreams limits how many streams a single HTTP/2 connection
	// can open. The value is carried over from the x/net/http2 configuration
	// this server used before migrating to the net/http HTTP/2 support.
	maxConcurrentStreams = 250

	// serverIdleTimeout bounds how long an idle connection is kept alive.
	// net/http derives the HTTP/2 idle timeout from http.Server.IdleTimeout,
	// so this single value covers both HTTP/1.1 and HTTP/2. Long-lived
	// streaming RPCs (Subscribe, SubscribeInteractions) may be idle for minutes
	// between messages, so keep it generous.
	serverIdleTimeout = 10 * time.Minute
)

type Server struct {
	server                        *http.Server
	env                           *config.Env
	projectServer                 *project.Server
	workflowServer                *workflow.Server
	taskServer                    *task.Server
	interactionServer             *interaction.Server
	agentManagerServer            *agentmanager.Server
	agentServer                   *agent.Server
	skillServer                   *skill.Server
	scriptServer                  *script.Server
	eventServer                   *event.Server
	taskLogServer                 *tasklog.Server
	pushNotificationServer        *pushnotification.Server
	permissionServer              *permission.Server
	singleCommandPermissionServer *singlecommandpermission.Server
	templateServer                *tmpl.Server
	claudeSettingsServer          *claudesettings.Server
	scheduleServer                *schedule.Server
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
	scriptServer *script.Server,
	eventServer *event.Server,
	taskLogServer *tasklog.Server,
	pushNotificationServer *pushnotification.Server,
	permissionServer *permission.Server,
	singleCommandPermissionServer *singlecommandpermission.Server,
	templateServer *tmpl.Server,
	claudeSettingsServer *claudesettings.Server,
	scheduleServer *schedule.Server,
) *Server {
	return &Server{
		env:                           env,
		projectServer:                 projectServer,
		workflowServer:                workflowServer,
		taskServer:                    taskServer,
		interactionServer:             interactionServer,
		agentManagerServer:            agentManagerServer,
		agentServer:                   agentServer,
		skillServer:                   skillServer,
		scriptServer:                  scriptServer,
		eventServer:                   eventServer,
		taskLogServer:                 taskLogServer,
		pushNotificationServer:        pushNotificationServer,
		permissionServer:              permissionServer,
		singleCommandPermissionServer: singleCommandPermissionServer,
		templateServer:                templateServer,
		claudeSettingsServer:          claudeSettingsServer,
		scheduleServer:                scheduleServer,
	}
}

// ListenAndServe starts the HTTP server on the configured host and port. The
// provided context is used as the base context for all incoming requests via
// http.Server.BaseContext. When ctx is canceled (e.g. on shutdown signal), all
// streaming RPC contexts are also canceled, allowing the server to shut down
// without waiting for streams.
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := net.JoinHostPort(s.env.HTTPHost, s.env.HTTPPort)

	var lc net.ListenConfig

	// ctx only bounds the bind itself; canceling it later does not close ln.
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	return s.Serve(ctx, ln)
}

// Serve accepts connections on ln and serves the Connect API over both
// HTTP/1.1 and cleartext HTTP/2 (h2c). It is ListenAndServe with a
// caller-supplied listener; tests use it to bind an ephemeral port.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.server = s.newHTTPServer(ctx)

	slog.Info("starting server", "addr", ln.Addr().String())

	return s.server.Serve(ln)
}

// Shutdown gracefully stops the server. It is a no-op when the server was
// never started: ListenAndServe returns before assigning s.server if the
// listener cannot bind, and callers shut down unconditionally on that error.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

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
		// Skip API key check for health endpoints and token-based interaction response.
		if r.URL.Path == "/health" || r.URL.Path == "/grpc.health.v1.Health/Check" ||
			r.URL.Path == "/taskguild.v1.InteractionService/RespondToInteractionByToken" {
			next.ServeHTTP(w, r)
			return
		}

		apiKey := r.Header.Get("X-Api-Key")
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

// newHTTPServer builds the production http.Server: the full handler chain
// (CORS -> API key middleware -> mux) plus the protocol configuration.
func (s *Server) newHTTPServer(ctx context.Context) *http.Server {
	// Accept HTTP/1.1 and cleartext HTTP/2 (h2c) on the same port. gRPC and
	// Connect clients speaking h2c open the connection with the HTTP/2 client
	// preface, which net/http detects and hands off to its HTTP/2 server. This
	// replaces the deprecated golang.org/x/net/http2/h2c handler wrapper.
	//
	// HTTP/2 over TLS is deliberately not enabled: TLS is always terminated
	// upstream and this server only ever runs on a plaintext TCP listener.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	handler := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	}).Handler(s.apiKeyMiddleware(s.newMux()))

	return &http.Server{
		Handler:   handler,
		Protocols: protocols,
		// http.HTTP2Config has no IdleTimeout field; net/http derives the
		// HTTP/2 idle timeout from Server.IdleTimeout below.
		HTTP2: &http.HTTP2Config{
			MaxConcurrentStreams: maxConcurrentStreams,
		},
		BaseContext: func(_ net.Listener) context.Context { return ctx },
		IdleTimeout: serverIdleTimeout,
	}
}

// newMux builds the request router: the chi sub-router mounted at /api, the
// health endpoints, and every Connect service handler.
func (s *Server) newMux() http.Handler {
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
	mux.Handle(taskguildv1connect.NewScriptServiceHandler(s.scriptServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewEventServiceHandler(s.eventServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewTaskLogServiceHandler(s.taskLogServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewPushNotificationServiceHandler(s.pushNotificationServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewPermissionServiceHandler(s.permissionServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewSingleCommandPermissionServiceHandler(s.singleCommandPermissionServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewTemplateServiceHandler(s.templateServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewClaudeSettingsServiceHandler(s.claudeSettingsServer, handlerOpts))
	mux.Handle(taskguildv1connect.NewScheduleServiceHandler(s.scheduleServer, handlerOpts))

	return mux
}
