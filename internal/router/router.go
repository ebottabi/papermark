/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package router

import (
	"context"
	"errors"
	"log"
	"time"

	"papermark/internal/controllers"
	"papermark/internal/database"
	"papermark/internal/handlers"
	"papermark/internal/middleware"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Config carries what the HTTP layer needs: infrastructure for /ready, the
// session lookup, and one controller per resource. A nil controller leaves
// its routes unregistered.
type Config struct {
	Authenticate middleware.Authenticate
	BodyLimit    int
	Quiet        bool // skip the request logger (tests)

	Pool  *pgxpool.Pool
	Redis *redis.Client // nil when Redis is not configured

	Tasks       *controllers.TaskController
	Projects    *controllers.ProjectController
	Knowledge   *controllers.KnowledgeController
	Skills      *controllers.SkillController
	Devices     *controllers.DeviceController
	Schedules   *controllers.ScheduleController
	Connectors  *controllers.ConnectorController
	GmailLabels *controllers.GmailLabelController
	Auth        *controllers.AuthController
	Prefs       *controllers.PreferencesController
	Updates     *controllers.UpdateController
	Memories    *controllers.MemoryController
	Flights     *controllers.FlightController // nil: no flight routes
}

// New builds the Fiber app: error envelope, recover, logger, CORS, /health,
// /ready and every /v1 route. cmd/server and the HTTP tests both use it.
func New(cfg *Config) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:               "Papermark Agent Server",
		BodyLimit:             cfg.BodyLimit,
		DisableStartupMessage: true,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			status := fiber.StatusInternalServerError
			var fe *fiber.Error
			if errors.As(err, &fe) {
				status = fe.Code
			}
			if status >= 500 {
				log.Printf("Error handling %s %s status=%d err=%v", c.Method(), c.Path(), status, err)
			}
			return c.Status(status).JSON(handlers.ErrorResponse{
				Success: false,
				Error:   err.Error(),
				Code:    handlers.CodeForStatus(status),
			})
		},
	})

	app.Use(recover.New())
	if !cfg.Quiet {
		app.Use(logger.New(logger.Config{
			Format:     "${time} | ${status} | ${latency} | ${method} ${path}\n",
			TimeFormat: "2006-01-02 15:04:05",
		}))
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, Last-Event-ID",
		AllowMethods: "GET, POST, PUT, PATCH, DELETE, OPTIONS",
	}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":    "healthy",
			"service":   "papermark-agent-server",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})

	app.Get("/ready", func(c *fiber.Ctx) error {
		checks := fiber.Map{"database": "ok", "redis": "ok"}
		ready := true
		if err := database.PoolHealthCheck(context.Background(), cfg.Pool); err != nil {
			checks["database"] = "unavailable"
			ready = false
		}
		if cfg.Redis != nil {
			if err := database.RedisHealthCheck(cfg.Redis); err != nil {
				checks["redis"] = "unavailable"
				ready = false
			}
		} else {
			checks["redis"] = "not_configured"
		}
		status, statusCode := "ready", fiber.StatusOK
		if !ready {
			status, statusCode = "not_ready", fiber.StatusServiceUnavailable
		}
		return c.Status(statusCode).JSON(fiber.Map{"status": status, "checks": checks})
	})

	Setup(app, cfg)

	app.All("/v1/*", func(c *fiber.Ctx) error { return fiber.ErrNotFound })
	return app
}

// Setup registers every /v1 route. Add a setupXRoutes call per resource.
func Setup(app *fiber.App, cfg *Config) {
	api := app.Group("/v1")

	setupOAuthCallbackRoute(api, cfg)
	setupPayReturnRoute(api, cfg)
	setupPublicAuthRoutes(api, cfg)

	protected := api.Group("")
	protected.Use(middleware.AuthMiddleware(cfg.Authenticate))

	protected.Get("/status", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true, "data": fiber.Map{
			"service": "papermark-agent-server", "version": "1.0.0", "status": "operational",
		}})
	})

	setupTaskRoutes(protected, cfg.Tasks)
	setupProjectRoutes(protected, cfg)
	setupDeviceRoutes(protected, cfg)
	setupScheduleRoutes(protected, cfg)
	setupFlightRoutes(protected, cfg)
	setupConnectorRoutes(protected, cfg)
	setupGmailLabelRoutes(protected, cfg)
	setupAuthRoutes(protected, cfg)
	setupAssistantRoutes(protected, cfg)
}

func setupAssistantRoutes(r fiber.Router, cfg *Config) {
	if h := cfg.Prefs; h != nil {
		r.Get("/me/preferences", h.Get)
		r.Patch("/me/preferences", h.Patch)
	}
	if h := cfg.Updates; h != nil {
		r.Get("/updates", h.List)
		r.Post("/updates/read-all", h.ReadAll)
		r.Post("/updates/:id/read", h.Read)
	}
	if h := cfg.Memories; h != nil {
		r.Get("/memories", h.List)
		r.Post("/memories", h.Create)
		r.Patch("/memories/:id", h.Update)
		r.Delete("/memories/:id", h.Delete)
	}
}

// setupPublicAuthRoutes registers the sign-in endpoints. They sit outside
// session auth, so they are rate limited per IP.
func setupPublicAuthRoutes(api fiber.Router, cfg *Config) {
	h := cfg.Auth
	if h == nil {
		return
	}
	limit := middleware.RateLimit(20, time.Minute)
	api.Post("/auth/google", limit, h.Google)
	api.Post("/auth/passkeys/login/begin", limit, h.PasskeyLoginBegin)
	api.Post("/auth/passkeys/login/finish", limit, h.PasskeyLoginFinish)
	api.Post("/auth/faceid/challenge", limit, h.FaceIDChallenge)
	api.Post("/auth/faceid/login", limit, h.FaceIDLogin)
}

func setupAuthRoutes(r fiber.Router, cfg *Config) {
	h := cfg.Auth
	if h == nil {
		return
	}
	limit := middleware.RateLimit(30, time.Minute)
	r.Get("/me", h.Me)
	r.Get("/auth/methods", h.Methods)
	r.Post("/auth/logout", h.Logout)
	r.Post("/auth/passkeys/register/begin", limit, h.PasskeyRegisterBegin)
	r.Post("/auth/passkeys/register/finish", limit, h.PasskeyRegisterFinish)
	r.Get("/auth/passkeys", h.Passkeys)
	r.Delete("/auth/passkeys/:id", h.DeletePasskey)
	r.Post("/auth/faceid/register", limit, h.FaceIDRegister)
	r.Get("/auth/faceid", h.FaceIDKeys)
	r.Delete("/auth/faceid/:id", h.RevokeFaceIDKey)
}

func setupTaskRoutes(r fiber.Router, h *controllers.TaskController) {
	if h == nil {
		return
	}
	r.Get("/usage", h.Usage)
	r.Get("/tasks", h.List)
	r.Post("/tasks", h.Create)
	r.Get("/tasks/:id", h.Get)
	r.Patch("/tasks/:id", h.Patch)
	r.Get("/tasks/:id/events", h.Events)
	r.Post("/tasks/:id/reply", h.Reply)
	r.Post("/tasks/:id/confirm", h.Confirm)
	r.Post("/tasks/:id/messages", h.Message)
	r.Post("/tasks/:id/stop", h.Stop)
	r.Post("/tasks/:id/fork", h.Fork)
	r.Get("/tasks/:id/files", h.Files)
	r.Get("/tasks/:id/files/*", h.File)
	r.Get("/tasks/:id/transcript", h.Transcript)
}

func setupProjectRoutes(r fiber.Router, cfg *Config) {
	if h := cfg.Projects; h != nil {
		r.Get("/projects", h.List)
		r.Post("/projects", h.Create)
		r.Patch("/projects/:id", h.Update)
		r.Delete("/projects/:id", h.Delete)
	}
	if h := cfg.Knowledge; h != nil {
		r.Get("/knowledge", h.List)
		r.Post("/knowledge", h.Create)
		r.Patch("/knowledge/:id", h.Update)
		r.Delete("/knowledge/:id", h.Delete)
	}
	if h := cfg.Skills; h != nil {
		r.Get("/skills", h.List)
		r.Post("/skills", h.Create)
		r.Patch("/skills/:id", h.Update)
		r.Delete("/skills/:id", h.Delete)
	}
}

func setupDeviceRoutes(api fiber.Router, cfg *Config) {
	if cfg.Devices == nil {
		return
	}
	api.Post("/devices", cfg.Devices.Register)
	api.Delete("/devices/:token", cfg.Devices.Unregister)
}

func setupScheduleRoutes(api fiber.Router, cfg *Config) {
	if cfg.Schedules == nil {
		return
	}
	api.Get("/schedules", cfg.Schedules.List)
	api.Post("/schedules", cfg.Schedules.Create)
	api.Post("/schedules/draft", cfg.Schedules.Draft)
	api.Patch("/schedules/:id", cfg.Schedules.Update)
	api.Delete("/schedules/:id", cfg.Schedules.Delete)
	api.Post("/schedules/:id/run", cfg.Schedules.Run)
	api.Get("/schedules/:id/triggers", cfg.Schedules.Triggers)
}

func setupFlightRoutes(api fiber.Router, cfg *Config) {
	f := cfg.Flights
	if f == nil {
		return
	}
	api.Get("/flights/searches/:id/offers", f.Offers)
	api.Get("/flights/searches/:id/offers/:ref", f.Offer)
	api.Post("/checkouts", f.CreateCheckout)
	api.Get("/checkouts/:id", f.GetCheckout)
	api.Patch("/checkouts/:id", f.PatchCheckout)
	api.Get("/checkouts/:id/extras", f.Extras)
	api.Post("/checkouts/:id/hold", f.Hold)
	api.Post("/checkouts/:id/handoff", f.Handoff)
	api.Post("/checkouts/:id/pay", f.Pay)
	api.Post("/checkouts/:id/pay/confirm", f.ConfirmPay)
	api.Get("/trips", f.Trips)
	api.Get("/travellers", f.Travellers)
	api.Post("/travellers", f.CreateTraveller)
	api.Patch("/travellers/:id", f.PatchTraveller)
	api.Delete("/travellers/:id", f.DeleteTraveller)
}

// setupOAuthCallbackRoute registers the public OAuth redirect target; it
// sits outside bearer auth, so it is rate limited per IP.
func setupOAuthCallbackRoute(api fiber.Router, cfg *Config) {
	if cfg.Connectors == nil {
		return
	}
	api.Get("/oauth/callback", middleware.RateLimit(30, time.Minute), cfg.Connectors.Callback)
}

// setupPayReturnRoute registers the public Stripe return target; like the OAuth
// callback it sits outside bearer auth and is rate limited per IP.
func setupPayReturnRoute(api fiber.Router, cfg *Config) {
	if cfg.Flights == nil {
		return
	}
	api.Get("/checkouts/pay/return", middleware.RateLimit(30, time.Minute), cfg.Flights.PayReturn)
}

func setupConnectorRoutes(api fiber.Router, cfg *Config) {
	if cfg.Connectors == nil {
		return
	}
	api.Get("/connectors", cfg.Connectors.List)
	api.Post("/connectors", cfg.Connectors.Add)
	// :name is gmail or calendar; the service answers 404 for an account of another connector.
	api.Post("/connectors/:name/accounts/:id/authorize", cfg.Connectors.AuthorizeAccount)
	api.Post("/connectors/:name/accounts/:id/primary", cfg.Connectors.SetPrimary)
	api.Delete("/connectors/:name/accounts/:id", cfg.Connectors.DeleteAccount)
	api.Delete("/connectors/:name", cfg.Connectors.Delete)
	api.Post("/connectors/:name/authorize", cfg.Connectors.Authorize)
}

func setupGmailLabelRoutes(api fiber.Router, cfg *Config) {
	if cfg.GmailLabels == nil {
		return
	}
	api.Get("/connectors/gmail/accounts/:id/labels", cfg.GmailLabels.List)
}
