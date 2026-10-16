/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

// Command worker runs only the background job worker, for deployments that
// scale it apart from cmd/server. It needs Redis so live events reach the
// HTTP servers.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"papermark/internal/config"
	"papermark/internal/database"
	"papermark/internal/services"
	"papermark/internal/services/jobs"
	"papermark/internal/workers"
)

func main() {
	log.Println("==============================================")
	log.Println("Starting Papermark Job Worker")
	log.Println("==============================================")

	ctx := context.Background()
	cfg := config.Load()

	pool, err := database.InitPool(ctx, database.LoadPostgresConfig())
	if err != nil {
		log.Fatalf("Failed to create database pool: %v", err)
	}
	log.Println("PostgreSQL pool ready")

	redisClient, err := database.InitRedis(database.LoadRedisConfig())
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Redis connected")

	svcs := services.New(cfg, pool, redisClient)
	worker := jobs.NewWorker(svcs.Jobs, workers.NewRegistry(svcs), jobs.WorkerOptions{
		Concurrency:  cfg.Worker.Concurrency,
		Lease:        cfg.Worker.Lease,
		PollInterval: cfg.Worker.PollInterval,
	})

	workerCtx, cancel := context.WithCancel(context.Background())
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		log.Println("Shutdown signal received")
		cancel()
	}()
	go workers.RunScheduler(workerCtx, svcs.Schedules, workers.SchedulerInterval)

	log.Println("Worker ready, consuming jobs...")
	if err := worker.Run(workerCtx); err != nil && err != context.Canceled {
		log.Fatalf("Worker failed: %v", err)
	}

	database.ClosePool(pool)
	_ = database.CloseRedis(redisClient)
	log.Println("Worker stopped")
}
