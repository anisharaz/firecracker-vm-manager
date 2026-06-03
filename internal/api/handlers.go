package api

import (
	"errors"

	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/manager"

	"github.com/gofiber/fiber/v2"
)

// Server bundles the manager into Fiber handlers.
type Server struct {
	mgr *manager.Manager
}

func NewServer(mgr *manager.Manager) *Server { return &Server{mgr: mgr} }

func (s *Server) handleHealth(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"ok": true})
}

func (s *Server) handleCreate(c *fiber.Ctx) error {
	var req CreateVMRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid json body")
	}
	v, err := s.mgr.Create(c.UserContext(), manager.CreateRequest{
		Name:        req.Name,
		VCPUs:       req.VCPUs,
		MemMiB:      req.MemMiB,
		DiskSizeMiB: req.DiskSizeMiB,
		KernelArgs:  req.KernelArgs,
	})
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(toResponse(v))
}

func (s *Server) handleList(c *fiber.Ctx) error {
	vms := s.mgr.List(c.UserContext())
	out := make([]VMResponse, 0, len(vms))
	for _, v := range vms {
		out = append(out, toResponse(v))
	}
	return c.JSON(out)
}

func (s *Server) handleGet(c *fiber.Ctx) error {
	v, err := s.mgr.Get(c.UserContext(), c.Params("id"))
	if err != nil {
		return mapError(err)
	}
	return c.JSON(toResponse(v))
}

func (s *Server) handleDelete(c *fiber.Ctx) error {
	if err := s.mgr.Delete(c.UserContext(), c.Params("id")); err != nil {
		return mapError(err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) handleStart(c *fiber.Ctx) error {
	v, err := s.mgr.Start(c.UserContext(), c.Params("id"))
	if err != nil {
		return mapError(err)
	}
	return c.JSON(toResponse(v))
}

func (s *Server) handleStop(c *fiber.Ctx) error {
	v, err := s.mgr.Stop(c.UserContext(), c.Params("id"))
	if err != nil {
		return mapError(err)
	}
	return c.JSON(toResponse(v))
}

func (s *Server) handleKill(c *fiber.Ctx) error {
	v, err := s.mgr.Kill(c.UserContext(), c.Params("id"))
	if err != nil {
		return mapError(err)
	}
	return c.JSON(toResponse(v))
}

func (s *Server) handleRestart(c *fiber.Ctx) error {
	v, err := s.mgr.Restart(c.UserContext(), c.Params("id"))
	if err != nil {
		return mapError(err)
	}
	return c.JSON(toResponse(v))
}

func mapError(err error) error {
	if errors.Is(err, manager.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return fiber.NewError(fiber.StatusInternalServerError, err.Error())
}
