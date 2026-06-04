package api

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// NewApp builds the Fiber app with all routes wired in.
func NewApp(s *Server) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:               "firecracker-manager",
		DisableStartupMessage: true,
		ErrorHandler:          errorHandler,
	})

	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(logger.New())

	app.Get("/healthz", s.handleHealth)

	v := app.Group("/vms")
	v.Post("/", s.handleCreate)
	v.Get("/", s.handleList)
	v.Get("/:id", s.handleGet)
	v.Delete("/:id", s.handleDelete)
	v.Post("/:id/start", s.handleStart)
	v.Post("/:id/stop", s.handleStop)
	v.Post("/:id/kill", s.handleKill)
	v.Post("/:id/restart", s.handleRestart)

	v.Use("/:id/console", consoleUpgrade)
	v.Get("/:id/console", websocket.New(s.handleConsole))

	return app
}

func errorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	msg := err.Error()
	if fe, ok := err.(*fiber.Error); ok {
		code = fe.Code
		msg = fe.Message
	}
	return c.Status(code).JSON(errorBody{Error: msg})
}
