package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	domainerrors "github.com/erayozdayioglu/insider-one-notification-system/internal/domain/errors"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/repository"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/service"
)

const (
	defaultOffset = 0
	defaultLimit  = 20
	maxLimit      = 100
	maxBatchSize  = 1000
)

// NotificationHandler handles HTTP requests for the notification resource.
type NotificationHandler struct {
	svc service.NotificationService
}

// NewNotificationHandler creates a NotificationHandler backed by the given service.
func NewNotificationHandler(svc service.NotificationService) *NotificationHandler {
	return &NotificationHandler{svc: svc}
}

// Create godoc
// @Summary      Create a notification
// @Description  Create a single notification for asynchronous delivery.
// @Tags         notifications
// @Accept       json
// @Produce      json
// @Param        request body CreateNotificationRequest true "Notification payload"
// @Success      201 {object} CreateNotificationResponse
// @Failure      400 {object} ErrorResponse
// @Failure      409 {object} ErrorResponse "Duplicate idempotency key"
// @Failure      422 {object} ErrorResponse "Validation error"
// @Failure      500 {object} ErrorResponse
// @Router       /api/v1/notifications [post]
func (h *NotificationHandler) Create(c *gin.Context) {
	var req CreateNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	notif := h.mapCreateRequestToEntity(&req)

	if err := h.svc.Create(c.Request.Context(), notif); err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, CreateNotificationResponse{
		ID:        notif.ID.String(),
		Status:    string(notif.Status),
		CreatedAt: notif.CreatedAt,
	})
}

// CreateBatch godoc
// @Summary      Create a batch of notifications
// @Description  Create up to 1000 notifications in a single request.
// @Tags         notifications
// @Accept       json
// @Produce      json
// @Param        request body BatchCreateRequest true "Batch payload"
// @Success      201 {object} BatchCreateResponse
// @Failure      400 {object} ErrorResponse
// @Failure      409 {object} ErrorResponse "Duplicate idempotency key"
// @Failure      422 {object} ErrorResponse "Validation error"
// @Failure      500 {object} ErrorResponse
// @Router       /api/v1/notifications/batch [post]
func (h *NotificationHandler) CreateBatch(c *gin.Context) {
	var req BatchCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if len(req.Notifications) > maxBatchSize {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "batch size exceeds maximum of 1000",
		})
		return
	}

	entities := make([]*entity.Notification, 0, len(req.Notifications))
	for i := range req.Notifications {
		n := h.mapCreateRequestToEntity(&req.Notifications[i])
		entities = append(entities, n)
	}

	if err := h.svc.CreateBatch(c.Request.Context(), entities); err != nil {
		h.handleError(c, err)
		return
	}

	summaries := make([]BatchNotificationSummary, 0, len(entities))
	for _, n := range entities {
		summaries = append(summaries, BatchNotificationSummary{
			ID:     n.ID.String(),
			Status: string(n.Status),
		})
	}

	// BatchID is assigned by the service layer during CreateBatch.
	var batchID string
	if entities[0].BatchID != nil {
		batchID = entities[0].BatchID.String()
	}

	c.JSON(http.StatusCreated, BatchCreateResponse{
		BatchID:       batchID,
		Notifications: summaries,
		Total:         len(entities),
	})
}

// GetByID godoc
// @Summary      Get a notification by ID
// @Description  Retrieve a single notification by its UUID.
// @Tags         notifications
// @Produce      json
// @Param        id path string true "Notification ID" format(uuid)
// @Success      200 {object} NotificationResponse
// @Failure      400 {object} ErrorResponse
// @Failure      404 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /api/v1/notifications/{id} [get]
func (h *NotificationHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid notification id"})
		return
	}

	notif, err := h.svc.GetByID(c.Request.Context(), id)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, mapNotificationToResponse(notif))
}

// GetByBatchID godoc
// @Summary      Get notifications by batch ID
// @Description  Retrieve all notifications belonging to a batch with pagination.
// @Tags         notifications
// @Produce      json
// @Param        batchId path string true "Batch ID" format(uuid)
// @Param        offset  query int false "Offset" default(0)
// @Param        limit   query int false "Limit"  default(20)
// @Success      200 {object} PaginatedNotificationsResponse
// @Failure      400 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /api/v1/notifications/batch/{batchId} [get]
func (h *NotificationHandler) GetByBatchID(c *gin.Context) {
	batchID, err := uuid.Parse(c.Param("batchId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid batch id"})
		return
	}

	offset, limit := parsePagination(c)

	notifications, total, err := h.svc.GetByBatchID(c.Request.Context(), batchID, repository.ListParams{
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}

	resp := PaginatedNotificationsResponse{
		Notifications: mapNotificationsToResponse(notifications),
		Total:         total,
		Offset:        offset,
		Limit:         limit,
	}

	c.JSON(http.StatusOK, resp)
}

// Cancel godoc
// @Summary      Cancel a notification
// @Description  Cancel a pending or queued notification. Cannot cancel already delivered or failed notifications.
// @Tags         notifications
// @Produce      json
// @Param        id path string true "Notification ID" format(uuid)
// @Success      200 {object} CancelNotificationResponse
// @Failure      400 {object} ErrorResponse
// @Failure      404 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /api/v1/notifications/{id}/cancel [patch]
func (h *NotificationHandler) Cancel(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid notification id"})
		return
	}

	if err := h.svc.Cancel(c.Request.Context(), id); err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, CancelNotificationResponse{
		ID:     id.String(),
		Status: string(entity.StatusCancelled),
	})
}

// List godoc
// @Summary      List notifications
// @Description  List notifications with optional filters, sorting, and pagination.
// @Tags         notifications
// @Produce      json
// @Param        channel    query string false "Filter by channel" Enums(sms, email, push)
// @Param        status     query string false "Filter by status" Enums(pending, queued, processing, delivered, failed, cancelled)
// @Param        priority   query string false "Filter by priority" Enums(high, normal, low)
// @Param        recipient  query string false "Filter by recipient"
// @Param        offset     query int    false "Offset" default(0)
// @Param        limit      query int    false "Limit"  default(20)
// @Param        sort_by    query string false "Sort field" default(created_at)
// @Param        sort_order query string false "Sort direction" Enums(asc, desc) default(desc)
// @Success      200 {object} PaginatedNotificationsResponse
// @Failure      400 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /api/v1/notifications [get]
func (h *NotificationHandler) List(c *gin.Context) {
	offset, limit := parsePagination(c)

	filter := repository.ListFilter{
		Offset:    offset,
		Limit:     limit,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: repository.SortOrder(c.DefaultQuery("sort_order", "desc")),
	}

	if ch := c.Query("channel"); ch != "" {
		channel := entity.Channel(ch)
		filter.Channel = &channel
	}
	if st := c.Query("status"); st != "" {
		status := entity.Status(st)
		filter.Status = &status
	}
	if pr := c.Query("priority"); pr != "" {
		priority := entity.Priority(pr)
		filter.Priority = &priority
	}
	if r := c.Query("recipient"); r != "" {
		filter.Recipient = &r
	}

	notifications, total, err := h.svc.List(c.Request.Context(), filter)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, PaginatedNotificationsResponse{
		Notifications: mapNotificationsToResponse(notifications),
		Total:         total,
		Offset:        offset,
		Limit:         limit,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (h *NotificationHandler) mapCreateRequestToEntity(req *CreateNotificationRequest) *entity.Notification {
	notif := entity.NewNotification(
		entity.Channel(req.Channel),
		entity.Priority(req.Priority),
		req.Recipient,
		req.Content,
		req.IdempotencyKey,
	)

	notif.Subject = req.Subject
	notif.TemplateVars = req.TemplateVars
	notif.ScheduledAt = req.ScheduledAt

	if req.TemplateID != nil {
		if tid, err := uuid.Parse(*req.TemplateID); err == nil {
			notif.TemplateID = &tid
		}
	}

	return notif
}

func (h *NotificationHandler) handleError(c *gin.Context, err error) {
	mapDomainError(c, err)
}

func mapDomainError(c *gin.Context, err error) {
	var validationErr *domainerrors.ValidationError
	if errors.As(err, &validationErr) {
		c.JSON(http.StatusUnprocessableEntity, ErrorResponse{
			Error: "validation failed",
			Details: []ValidationErrorDetail{
				{Field: validationErr.Field, Message: validationErr.Message},
			},
		})
		return
	}

	var notFoundErr *domainerrors.NotFoundError
	if errors.As(err, &notFoundErr) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: notFoundErr.Error()})
		return
	}

	if errors.Is(err, domainerrors.ErrNotFound) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "not found"})
		return
	}

	if errors.Is(err, domainerrors.ErrDuplicateIdempotencyKey) {
		c.JSON(http.StatusConflict, ErrorResponse{Error: "duplicate idempotency key"})
		return
	}

	if errors.Is(err, domainerrors.ErrInvalidInput) {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if errors.Is(err, domainerrors.ErrRateLimited) {
		c.JSON(http.StatusTooManyRequests, ErrorResponse{Error: "rate limited"})
		return
	}

	c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "internal server error"})
}

func mapNotificationToResponse(n *entity.Notification) NotificationResponse {
	resp := NotificationResponse{
		ID:                n.ID.String(),
		IdempotencyKey:    n.IdempotencyKey,
		Channel:           string(n.Channel),
		Priority:          string(n.Priority),
		Recipient:         n.Recipient,
		Subject:           n.Subject,
		Content:           n.Content,
		TemplateVars:      n.TemplateVars,
		Status:            string(n.Status),
		ScheduledAt:       n.ScheduledAt,
		Attempts:          n.Attempts,
		MaxAttempts:       n.MaxAttempts,
		LastAttemptAt:     n.LastAttemptAt,
		NextRetryAt:       n.NextRetryAt,
		ProviderMessageID: n.ProviderMessageID,
		ErrorMessage:      n.ErrorMessage,
		CreatedAt:         n.CreatedAt,
		UpdatedAt:         n.UpdatedAt,
	}

	if n.BatchID != nil {
		s := n.BatchID.String()
		resp.BatchID = &s
	}
	if n.TemplateID != nil {
		s := n.TemplateID.String()
		resp.TemplateID = &s
	}

	return resp
}

func mapNotificationsToResponse(notifications []*entity.Notification) []NotificationResponse {
	result := make([]NotificationResponse, 0, len(notifications))
	for _, n := range notifications {
		result = append(result, mapNotificationToResponse(n))
	}
	return result
}

func parsePagination(c *gin.Context) (offset, limit int) {
	offset = defaultOffset
	limit = defaultLimit

	if v := c.Query("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			offset = parsed
		}
	}
	if v := c.Query("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	return offset, limit
}

