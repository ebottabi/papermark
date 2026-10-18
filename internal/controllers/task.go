/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, 15/10/2026
 */

package controllers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"papermark/internal/cache"
	"papermark/internal/handlers"
	"papermark/internal/middleware"
	"papermark/internal/responses"
	"papermark/internal/services"
	"papermark/internal/storage"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const noSuchTask = "no such task"

var contentTypes = map[string]string{
	".md": "text/markdown; charset=utf-8", ".txt": "text/plain; charset=utf-8", ".csv": "text/csv; charset=utf-8",
	".json": "application/json", ".html": "text/html; charset=utf-8", ".pdf": "application/pdf",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".svg": "image/svg+xml", ".zip": "application/zip",
}

// TaskController serves /tasks and /usage.
type TaskController struct {
	*handlers.BaseHandler
	tasks *services.TaskService
	bus   cache.Bus
}

func NewTaskController(tasks *services.TaskService, bus cache.Bus) *TaskController {
	return &TaskController{BaseHandler: &handlers.BaseHandler{}, tasks: tasks, bus: bus}
}

func (h *TaskController) fail(c *fiber.Ctx, err error) error {
	return sendServiceError(h.BaseHandler, c, err, noSuchTask)
}

func (h *TaskController) List(c *fiber.Ctx) error {
	var project *uuid.UUID
	if p := c.Query("projectId"); p != "" {
		id, err := uuid.Parse(p)
		if err != nil {
			return h.SendSuccess(c, fiber.Map{"tasks": []responses.TaskSummaryDTO{}})
		}
		project = &id
	}
	list, err := h.tasks.List(c.Context(), middleware.UserID(c), c.Query("archived") == "true", project)
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, fiber.Map{"tasks": responses.TaskSummariesResponse(list)})
}

func (h *TaskController) Create(c *fiber.Ctx) error {
	b, err := parseBody(c)
	if err != nil {
		return h.SendBadRequest(c, err.Error())
	}
	prompt, ok := b.str("task")
	if !ok || prompt == nil {
		return h.SendBadRequest(c, "task is required")
	}
	mode, ok := b.str("mode")
	if !ok {
		return h.SendBadRequest(c, "mode must be speed or quality")
	}
	in := services.NewTask{UserID: middleware.UserID(c), Prompt: *prompt, Mode: deref(mode), Attachments: b.raw("attachments")}
	if raw, present := b["projectId"]; present && raw != nil {
		s, ok := raw.(string)
		if !ok {
			return h.SendBadRequest(c, "projectId must be a string")
		}
		id, err := uuid.Parse(s)
		if err != nil {
			return h.SendBadRequest(c, "no such project")
		}
		in.ProjectID = &id
	}
	id, err := h.tasks.Create(c.Context(), in)
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendAccepted(c, fiber.Map{"id": id})
}

func (h *TaskController) Get(c *fiber.Ctx) error {
	id, ok := idParam(c)
	if !ok {
		return h.SendNotFound(c, noSuchTask)
	}
	t, err := h.tasks.Get(c.Context(), middleware.UserID(c), id)
	if err != nil {
		return h.fail(c, err)
	}
	events, err := h.tasks.Events(c.Context(), id, -1)
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, responses.TaskResponse(t, events))
}

func (h *TaskController) Patch(c *fiber.Ctx) error {
	id, ok := idParam(c)
	if !ok {
		return h.SendNotFound(c, noSuchTask)
	}
	b, err := parseBody(c)
	if err != nil {
		return h.SendBadRequest(c, err.Error())
	}
	favorite, ok := b.boolean("favorite")
	if !ok {
		return h.SendBadRequest(c, "favorite must be a boolean")
	}
	archived, ok := b.boolean("archived")
	if !ok {
		return h.SendBadRequest(c, "archived must be a boolean")
	}
	t, err := h.tasks.Patch(c.Context(), middleware.UserID(c), id, favorite, archived)
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, responses.TaskSummaryResponse(t))
}

func (h *TaskController) Reply(c *fiber.Ctx) error {
	return h.answer(c, "text", "text is required", h.tasks.Reply)
}

func (h *TaskController) Confirm(c *fiber.Ctx) error {
	return h.answer(c, "decision", "decision must be allow, always or deny", h.tasks.Confirm)
}

func (h *TaskController) answer(c *fiber.Ctx, field, missing string, call func(context.Context, uuid.UUID, uuid.UUID, string) error) error {
	id, ok := idParam(c)
	if !ok {
		return h.SendNotFound(c, noSuchTask)
	}
	b, err := parseBody(c)
	if err != nil {
		return h.SendBadRequest(c, err.Error())
	}
	v, ok := b.str(field)
	if !ok || v == nil {
		return h.SendBadRequest(c, missing)
	}
	if err := call(c.Context(), middleware.UserID(c), id, *v); err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, fiber.Map{"ok": true})
}

func (h *TaskController) Message(c *fiber.Ctx) error {
	id, ok := idParam(c)
	if !ok {
		return h.SendNotFound(c, noSuchTask)
	}
	b, err := parseBody(c)
	if err != nil {
		return h.SendBadRequest(c, err.Error())
	}
	text, ok := b.str("text")
	if !ok {
		return h.SendBadRequest(c, "text is required")
	}
	if err := h.tasks.Message(c.Context(), middleware.UserID(c), id, deref(text), b.raw("attachments")); err != nil {
		return h.fail(c, err)
	}
	return h.SendAccepted(c, fiber.Map{"ok": true})
}

func (h *TaskController) Stop(c *fiber.Ctx) error {
	id, ok := idParam(c)
	if !ok {
		return h.SendNotFound(c, noSuchTask)
	}
	if err := h.tasks.Stop(c.Context(), middleware.UserID(c), id); err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, fiber.Map{"ok": true})
}

func (h *TaskController) Fork(c *fiber.Ctx) error {
	id, ok := idParam(c)
	if !ok {
		return h.SendNotFound(c, noSuchTask)
	}
	b, err := parseBody(c)
	if err != nil {
		return h.SendBadRequest(c, err.Error())
	}
	prompt, ok := b.str("task")
	if !ok {
		return h.SendBadRequest(c, "task is required")
	}
	fork, err := h.tasks.Fork(c.Context(), middleware.UserID(c), id, deref(prompt))
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendAccepted(c, fiber.Map{"id": fork})
}

func (h *TaskController) Files(c *fiber.Ctx) error {
	id, ok := idParam(c)
	if !ok {
		return h.SendNotFound(c, noSuchTask)
	}
	files, err := h.tasks.Files(c.Context(), middleware.UserID(c), id)
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, fiber.Map{"files": responses.FilesResponse(files)})
}

// File streams one workspace file's raw bytes.
func (h *TaskController) File(c *fiber.Ctx) error {
	id, ok := idParam(c)
	if !ok {
		return h.SendNotFound(c, noSuchTask)
	}
	rel, err := url.PathUnescape(c.Params("*"))
	if err != nil {
		return h.SendBadRequest(c, "bad path")
	}
	f, info, err := h.tasks.OpenFile(c.Context(), middleware.UserID(c), id, rel)
	switch {
	case errors.Is(err, storage.ErrEscape):
		return h.SendBadRequest(c, "path escapes the workspace")
	case errors.Is(err, fs.ErrNotExist):
		return h.SendNotFound(c, "no such file")
	case err != nil:
		return h.fail(c, err)
	}
	ct, ok := contentTypes[strings.ToLower(filepath.Ext(info.Name()))]
	if !ok {
		ct = "application/octet-stream"
	}
	c.Set(fiber.HeaderContentType, ct)
	return c.SendStream(f, int(info.Size()))
}

func (h *TaskController) Transcript(c *fiber.Ctx) error {
	id, ok := idParam(c)
	if !ok {
		return h.SendNotFound(c, noSuchTask)
	}
	md, err := h.tasks.Transcript(c.Context(), middleware.UserID(c), id)
	if err != nil {
		return h.fail(c, err)
	}
	c.Set(fiber.HeaderContentType, "text/markdown; charset=utf-8")
	return c.SendString(md)
}

func (h *TaskController) Usage(c *fiber.Ctx) error {
	days, err := strconv.Atoi(c.Query("days"))
	if err != nil || days == 0 {
		days = 30
	}
	days = min(max(days, 1), 366)
	list, total, err := h.tasks.UsageByDay(c.Context(), middleware.UserID(c), days)
	if err != nil {
		return h.fail(c, err)
	}
	return h.SendSuccess(c, responses.UsageResponse(list, total))
}

const pingEvery = 20 * time.Second

// Events streams a task's events as SSE: everything after Last-Event-ID (or
// ?from), then live frames. It subscribes before reading Postgres so nothing
// falls between the replay and the live stream, and refills from Postgres
// whenever a live frame skips a sequence number.
func (h *TaskController) Events(c *fiber.Ctx) error {
	id, ok := idParam(c)
	if !ok {
		return h.SendNotFound(c, noSuchTask)
	}
	from := c.Get("Last-Event-ID", c.Query("from"))
	last, err := strconv.Atoi(from)
	if err != nil {
		last = -1
	}
	if _, err := h.tasks.Get(c.Context(), middleware.UserID(c), id); err != nil {
		return h.fail(c, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	frames, unsubscribe, err := h.bus.Subscribe(ctx, id.String())
	if err != nil {
		cancel()
		return h.fail(c, err)
	}
	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set(fiber.HeaderConnection, "keep-alive")
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer cancel()
		defer unsubscribe()
		ping := time.NewTicker(pingEvery)
		defer ping.Stop()
		refill := func() error {
			events, err := h.tasks.Events(ctx, id, last)
			if err != nil {
				return err
			}
			for _, e := range events {
				writeEvent(w, e.Seq, e.Data)
				last = e.Seq
			}
			return w.Flush()
		}
		if refill() != nil {
			return
		}
		// The replay is done, so a client can now tell a finished task from one it has only part of.
		_, _ = io.WriteString(w, "event: ready\ndata: {}\n\n")
		if w.Flush() != nil {
			return
		}
		for {
			select {
			case f, ok := <-frames:
				if !ok {
					return
				}
				switch {
				case f.Kind == cache.FrameDelta:
					text, _ := json.Marshal(map[string]string{"text": f.Text})
					_, _ = io.WriteString(w, "event: delta\ndata: "+string(text)+"\n\n")
				case f.Kind == cache.FrameActivity:
					name, _ := json.Marshal(map[string]string{"name": f.Text})
					_, _ = io.WriteString(w, "event: activity\ndata: "+string(name)+"\n\n")
				case f.Kind != cache.FrameEvent || f.Seq <= last:
					continue
				case f.Seq == last+1:
					writeEvent(w, f.Seq, f.Event)
					last = f.Seq
				default:
					if refill() != nil {
						return
					}
				}
			case <-ping.C:
				// The bus drops frames for a slow reader; a gap is otherwise only noticed at the next event.
				if refill() != nil {
					return
				}
				_, _ = io.WriteString(w, ": ping\n\n")
			}
			if w.Flush() != nil {
				return
			}
		}
	})
	return nil
}

func writeEvent(w io.Writer, seq int, data json.RawMessage) {
	_, _ = io.WriteString(w, "id: "+strconv.Itoa(seq)+"\ndata: "+string(data)+"\n\n")
}
