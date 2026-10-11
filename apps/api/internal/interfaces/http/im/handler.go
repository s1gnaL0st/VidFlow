package interfaceshttpim

import (
	applicationim "GCFeed/internal/application/im"
	domainim "GCFeed/internal/domain/im"
	infrajwt "GCFeed/internal/infra/jwt"
	infraws "GCFeed/internal/infra/ws"
	interfaceshttpmiddleware "GCFeed/internal/interfaces/http/middleware"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service    *applicationim.Service
	hub        *infraws.Hub
	jwtManager *infrajwt.Manager
}

func New(service *applicationim.Service, hub *infraws.Hub, jwtManager *infrajwt.Manager) *Handler {
	return &Handler{service: service, hub: hub, jwtManager: jwtManager}
}

func (h *Handler) CreateConversation(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
		return
	}
	var req createConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	conversationType, err := domainim.NormalizeConversationType(req.Type)
	if err != nil {
		writeIMError(c, err)
		return
	}
	var result *applicationim.CreateConversationResult
	switch conversationType {
	case domainim.ConversationTypeDirect:
		result, err = h.service.CreateDirectConversation(c.Request.Context(), userID, req.TargetUserID)
	case domainim.ConversationTypeGroup:
		result, err = h.service.CreateGroupConversation(c.Request.Context(), userID, req.Title, req.MemberIDs)
	}
	if err != nil {
		writeIMError(c, err)
		return
	}
	status := http.StatusCreated
	if !result.Created {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"conversation": conversationResponse{
		ID:        result.Conversation.ID,
		Type:      result.Conversation.Type,
		Title:     result.Conversation.Title,
		OwnerID:   result.Conversation.OwnerID,
		CreatedAt: result.Conversation.CreatedAt,
		UpdatedAt: result.Conversation.UpdatedAt,
	}})
}

func (h *Handler) ListConversations(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
		return
	}
	limit, err := parseNonNegativeInt(c.Query("limit"))
	if err != nil {
		writeIMError(c, err)
		return
	}
	items, err := h.service.ListConversations(c.Request.Context(), userID, limit)
	if err != nil {
		writeIMError(c, err)
		return
	}
	onlineByUserID := map[int64]bool{}
	for _, item := range items {
		if item == nil || item.Conversation == nil {
			continue
		}
		for _, member := range item.Members {
			if member != nil {
				onlineByUserID[member.UserID] = h.hub.Online(member.UserID)
			}
		}
	}
	responses := make([]conversationResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, responseFromConversationSummary(item, onlineByUserID))
	}
	c.JSON(http.StatusOK, conversationListResponse{Items: responses})
}

func (h *Handler) ListMessages(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
		return
	}
	conversationID, err := parsePositiveInt64(c.Param("conversationId"))
	if err != nil {
		writeIMError(c, domainim.ErrInvalidConversationID)
		return
	}
	beforeSequence, err := parseNonNegativeInt64(c.Query("before_sequence"))
	if err != nil {
		writeIMError(c, err)
		return
	}
	limit, err := parseNonNegativeInt(c.Query("limit"))
	if err != nil {
		writeIMError(c, err)
		return
	}
	items, err := h.service.ListMessages(c.Request.Context(), userID, conversationID, beforeSequence, limit)
	if err != nil {
		writeIMError(c, err)
		return
	}
	responses := make([]messageResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, responseFromMessage(item))
	}
	c.JSON(http.StatusOK, messageListResponse{Items: responses})
}

func (h *Handler) SendMessage(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
		return
	}
	conversationID, err := parsePositiveInt64(c.Param("conversationId"))
	if err != nil {
		writeIMError(c, domainim.ErrInvalidConversationID)
		return
	}
	var req sendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	messageType, err := domainim.NormalizeMessageType(req.Type)
	if err != nil {
		writeIMError(c, err)
		return
	}

	var result *applicationim.MessageResult
	switch messageType {
	case domainim.MessageTypeText:
		result, err = h.service.SendTextMessage(c.Request.Context(), userID, conversationID, req.Content, req.ClientMessageID)
	case domainim.MessageTypeVideo:
		result, err = h.service.SendVideoMessage(c.Request.Context(), userID, conversationID, req.Content, req.VideoID, req.ClientMessageID)
	default:
		err = domainim.ErrInvalidMessageType
	}
	if err != nil {
		writeIMError(c, err)
		return
	}
	response := responseFromMessage(result.Message)
	h.broadcastMessage(c.Request.Context(), result.RecipientIDs, response)
	status := http.StatusCreated
	if !result.Created {
		status = http.StatusOK
	}
	c.JSON(status, response)
}

func (h *Handler) MarkRead(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
		return
	}
	conversationID, err := parsePositiveInt64(c.Param("conversationId"))
	if err != nil {
		writeIMError(c, domainim.ErrInvalidConversationID)
		return
	}
	var req markReadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	count, err := h.service.MarkRead(c.Request.Context(), userID, conversationID, req.Sequence)
	if err != nil {
		writeIMError(c, err)
		return
	}
	c.JSON(http.StatusOK, markReadResponse{UpdatedCount: count})
}

func (h *Handler) WebSocket(c *gin.Context) {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "access token is required"})
		return
	}
	claims, err := h.jwtManager.ParseAndValidateToken(token, infrajwt.TokenTypeAccess)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid access token"})
		return
	}
	if err := h.hub.Serve(c.Writer, c.Request, claims.UserID); err != nil {
		return
	}
}

func (h *Handler) broadcastMessage(ctx context.Context, recipientIDs []int64, message messageResponse) {
	if h.hub == nil || len(recipientIDs) == 0 {
		return
	}
	payload, err := json.Marshal(websocketEnvelope{
		Type: "message.created",
		Data: &message,
	})
	if err != nil {
		return
	}
	h.hub.Broadcast(ctx, recipientIDs, payload)
}

func userIDFromContext(c *gin.Context) (int64, bool) {
	value, exists := c.Get(interfaceshttpmiddleware.ContextUserIDKey)
	if !exists {
		return 0, false
	}
	userID, ok := value.(int64)
	return userID, ok && userID > 0
}

func parsePositiveInt64(raw string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0, domainim.ErrInvalidConversationID
	}
	return value, nil
}

func parseNonNegativeInt64(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, domainim.ErrInvalidSequence
	}
	return value, nil
}

func parseNonNegativeInt(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, domainim.ErrInvalidLimit
	}
	return value, nil
}

func writeIMError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domainim.ErrConversationNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, domainim.ErrUserNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, domainim.ErrNotConversationMember):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case isBadRequestError(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

func isBadRequestError(err error) bool {
	return errors.Is(err, domainim.ErrInvalidUserID) ||
		errors.Is(err, domainim.ErrInvalidConversationID) ||
		errors.Is(err, domainim.ErrInvalidMessageID) ||
		errors.Is(err, domainim.ErrInvalidVideoID) ||
		errors.Is(err, domainim.ErrInvalidConversationType) ||
		errors.Is(err, domainim.ErrInvalidMessageType) ||
		errors.Is(err, domainim.ErrEmptyTitle) ||
		errors.Is(err, domainim.ErrTitleTooLong) ||
		errors.Is(err, domainim.ErrEmptyContent) ||
		errors.Is(err, domainim.ErrContentTooLong) ||
		errors.Is(err, domainim.ErrClientMessageIDTooLong) ||
		errors.Is(err, domainim.ErrInvalidLimit) ||
		errors.Is(err, domainim.ErrInvalidSequence) ||
		errors.Is(err, domainim.ErrDirectConversationWithSelf)
}
