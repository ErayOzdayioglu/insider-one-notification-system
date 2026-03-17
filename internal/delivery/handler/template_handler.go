package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/service"
)

// TemplateHandler handles HTTP requests for the template resource.
type TemplateHandler struct {
	svc service.TemplateService
}

// NewTemplateHandler creates a TemplateHandler backed by the given service.
func NewTemplateHandler(svc service.TemplateService) *TemplateHandler {
	return &TemplateHandler{svc: svc}
}

// Create godoc
// @Summary      Create a template
// @Description  Create a new message template with variable placeholders.
// @Tags         templates
// @Accept       json
// @Produce      json
// @Param        request body CreateTemplateRequest true "Template payload"
// @Success      201 {object} TemplateResponse
// @Failure      400 {object} ErrorResponse
// @Failure      422 {object} ErrorResponse "Validation error"
// @Failure      500 {object} ErrorResponse
// @Router       /api/v1/templates [post]
func (h *TemplateHandler) Create(c *gin.Context) {
	var req CreateTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	tmpl := entity.NewTemplate(req.Name, entity.Channel(req.Channel), req.Content)
	tmpl.Subject = req.Subject

	if err := h.svc.Create(c.Request.Context(), tmpl); err != nil {
		mapDomainError(c, err)
		return
	}

	c.JSON(http.StatusCreated, mapTemplateToResponse(tmpl))
}

// GetByID godoc
// @Summary      Get a template by ID
// @Description  Retrieve a single template by its UUID.
// @Tags         templates
// @Produce      json
// @Param        id path string true "Template ID" format(uuid)
// @Success      200 {object} TemplateResponse
// @Failure      400 {object} ErrorResponse
// @Failure      404 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /api/v1/templates/{id} [get]
func (h *TemplateHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid template id"})
		return
	}

	tmpl, err := h.svc.GetByID(c.Request.Context(), id)
	if err != nil {
		mapDomainError(c, err)
		return
	}

	c.JSON(http.StatusOK, mapTemplateToResponse(tmpl))
}

// Update godoc
// @Summary      Update a template
// @Description  Update an existing template's mutable fields.
// @Tags         templates
// @Accept       json
// @Produce      json
// @Param        id      path string               true "Template ID" format(uuid)
// @Param        request body UpdateTemplateRequest true "Fields to update"
// @Success      200 {object} TemplateResponse
// @Failure      400 {object} ErrorResponse
// @Failure      404 {object} ErrorResponse
// @Failure      422 {object} ErrorResponse "Validation error"
// @Failure      500 {object} ErrorResponse
// @Router       /api/v1/templates/{id} [put]
func (h *TemplateHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid template id"})
		return
	}

	var req UpdateTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	// Fetch existing template to apply partial update.
	tmpl, err := h.svc.GetByID(c.Request.Context(), id)
	if err != nil {
		mapDomainError(c, err)
		return
	}

	if req.Name != nil {
		tmpl.Name = *req.Name
	}
	if req.Channel != nil {
		tmpl.Channel = entity.Channel(*req.Channel)
	}
	if req.Subject != nil {
		tmpl.Subject = req.Subject
	}
	if req.Content != nil {
		tmpl.Content = *req.Content
		tmpl.Variables = entity.ExtractVariables(*req.Content)
	}
	if req.IsActive != nil {
		tmpl.IsActive = *req.IsActive
	}

	if err := h.svc.Update(c.Request.Context(), tmpl); err != nil {
		mapDomainError(c, err)
		return
	}

	c.JSON(http.StatusOK, mapTemplateToResponse(tmpl))
}

// Delete godoc
// @Summary      Delete a template
// @Description  Delete a template by its UUID.
// @Tags         templates
// @Produce      json
// @Param        id path string true "Template ID" format(uuid)
// @Success      204 "No Content"
// @Failure      400 {object} ErrorResponse
// @Failure      404 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /api/v1/templates/{id} [delete]
func (h *TemplateHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid template id"})
		return
	}

	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		mapDomainError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// List godoc
// @Summary      List templates
// @Description  List all templates with pagination.
// @Tags         templates
// @Produce      json
// @Param        offset query int false "Offset" default(0)
// @Param        limit  query int false "Limit"  default(20)
// @Success      200 {object} PaginatedTemplatesResponse
// @Failure      500 {object} ErrorResponse
// @Router       /api/v1/templates [get]
func (h *TemplateHandler) List(c *gin.Context) {
	offset, limit := parsePagination(c)

	templates, total, err := h.svc.List(c.Request.Context(), repository.ListParams{
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		mapDomainError(c, err)
		return
	}

	c.JSON(http.StatusOK, PaginatedTemplatesResponse{
		Templates: mapTemplatesToResponse(templates),
		Total:     total,
		Offset:    offset,
		Limit:     limit,
	})
}

// Render godoc
// @Summary      Render a template
// @Description  Render a template with the provided variables and return the result.
// @Tags         templates
// @Accept       json
// @Produce      json
// @Param        id      path string                true "Template ID" format(uuid)
// @Param        request body RenderTemplateRequest  true "Variables for substitution"
// @Success      200 {object} RenderTemplateResponse
// @Failure      400 {object} ErrorResponse
// @Failure      404 {object} ErrorResponse
// @Failure      422 {object} ErrorResponse "Missing required variables"
// @Failure      500 {object} ErrorResponse
// @Router       /api/v1/templates/{id}/render [post]
func (h *TemplateHandler) Render(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid template id"})
		return
	}

	var req RenderTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	subject, content, err := h.svc.Render(c.Request.Context(), id, req.Variables)
	if err != nil {
		mapDomainError(c, err)
		return
	}

	c.JSON(http.StatusOK, RenderTemplateResponse{
		Subject: subject,
		Content: content,
	})
}

// ---------------------------------------------------------------------------
// Mappers
// ---------------------------------------------------------------------------

func mapTemplateToResponse(t *entity.Template) TemplateResponse {
	vars := t.Variables
	if vars == nil {
		vars = []string{}
	}

	return TemplateResponse{
		ID:        t.ID.String(),
		Name:      t.Name,
		Channel:   string(t.Channel),
		Subject:   t.Subject,
		Content:   t.Content,
		Variables: vars,
		IsActive:  t.IsActive,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

func mapTemplatesToResponse(templates []*entity.Template) []TemplateResponse {
	result := make([]TemplateResponse, 0, len(templates))
	for _, t := range templates {
		result = append(result, mapTemplateToResponse(t))
	}
	return result
}
