/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

// Command server runs the HTTP API and, in the same process, the background
// job worker. Migrations run on boot.
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"papermark/internal/config"
	"papermark/internal/controllers"
	"papermark/internal/database"
	"papermark/internal/router"
	"papermark/internal/services"
	"papermark/internal/services/jobs"
	"papermark/internal/workers"

	"github.com/redis/go-redis/v9"
)

func main() {
	log.Println("==============================================")
	log.Println("Starting Papermark Agent Server")
	log.Println("==============================================")

	ctx := context.Background()
	cfg := config.Load()

	dbCfg := database.LoadPostgresConfig()
	migrator, err := database.InitMigrator(dbCfg)
	if err != nil {
		log.Fatalf("Failed to open migrator: %v", err)
	}
	if err := database.RunMigrations(migrator); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	if err := database.CloseMigrator(migrator); err != nil {
		log.Printf("Failed to close migrator: %v", err)
	}

	pool, err := database.InitPool(ctx, dbCfg)
	if err != nil {
		log.Fatalf("Failed to create database pool: %v", err)
	}
	log.Println("PostgreSQL pool ready")

	var redisClient *redis.Client
	if os.Getenv("REDIS_URL") != "" || os.Getenv("REDIS_HOST") != "" {
		redisClient, err = database.InitRedis(database.LoadRedisConfig())
		if err != nil {
			log.Fatalf("Failed to connect to Redis: %v", err)
		}
		log.Println("Redis connected")
	} else {
		log.Println("WARNING: Redis not configured; live events stay in this process")
	}

	svcs := services.New(cfg, pool, redisClient)

	app := router.New(&router.Config{
		Authenticate: svcs.Auth.Authenticate,
		BodyLimit:    cfg.BodyLimit,
		Pool:         pool,
		Redis:        redisClient,
		Tasks:        controllers.NewTaskController(svcs.Tasks, svcs.Bus),
		Projects:     controllers.NewProjectController(svcs.Projects),
		Knowledge:    controllers.NewKnowledgeController(svcs.Knowledge),
		Skills:       controllers.NewSkillController(svcs.Skills),
		Devices:      controllers.NewDeviceController(svcs.Devices),
		Schedules:    controllers.NewScheduleController(svcs.Schedules),
		Connectors:   controllers.NewConnectorController(svcs.MCP),
		GmailLabels:  controllers.NewGmailLabelController(services.NewGmailLabelService(pool, svcs.MCP)),
		Auth:         controllers.NewAuthController(svcs.Auth),
		Prefs:        controllers.NewPreferencesController(svcs.Prefs),
		Updates:      controllers.NewUpdateController(svcs.Updates),
		Memories:     controllers.NewMemoryController(svcs.Memories),
	})

	workerCtx, workerCancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	if cfg.Worker.InServer {
		worker := jobs.NewWorker(svcs.Jobs, workers.NewRegistry(svcs), jobs.WorkerOptions{
			Concurrency:  cfg.Worker.Concurrency,
			Lease:        cfg.Worker.Lease,
			PollInterval: cfg.Worker.PollInterval,
		})
		go func() {
			defer close(workerDone)
			if err := worker.Run(workerCtx); err != nil && err != context.Canceled {
				log.Printf("job worker error: %v", err)
			}
		}()
		go workers.RunScheduler(workerCtx, svcs.Schedules, workers.SchedulerInterval)
		go workers.RunTriggers(workerCtx, svcs.Triggers, workers.TriggerInterval)
	} else {
		log.Println("SERVER_BACKGROUND=off: jobs, schedules and triggers run in papermark-worker")
		close(workerDone)
	}

	ln, err := net.Listen("tcp", net.JoinHostPort(cfg.Host, cfg.Port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	// PORT=0 picks a free port; this line is how scripts and tests find it.
	log.Printf("Server listening on http://%s (API base: http://%s/v1)", ln.Addr(), ln.Addr())
	go func() {
		if err := app.Listener(ln); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	workerCancel()
	if err := app.ShutdownWithTimeout(30 * time.Second); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}
	<-workerDone
	database.ClosePool(pool)
	if err := database.CloseRedis(redisClient); err != nil {
		log.Printf("Error closing redis: %v", err)
	}
	log.Println("Server stopped")
}
