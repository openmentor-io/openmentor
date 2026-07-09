package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"go.uber.org/zap"
)

type MentorHandler struct {
	service services.MentorServiceInterface
	baseURL string
}

func NewMentorHandler(service services.MentorServiceInterface, baseURL string) *MentorHandler {
	return &MentorHandler{
		service: service,
		baseURL: baseURL,
	}
}

func (h *MentorHandler) GetPublicMentors(c *gin.Context) {
	mentors, err := h.service.GetAllMentors(c.Request.Context(), models.FilterOptions{
		OnlyVisible: true,
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to fetch mentors", err)
		return
	}

	publicMentors := make([]models.PublicMentorResponse, 0, len(mentors))
	for _, mentor := range mentors {
		publicMentors = append(publicMentors, mentor.ToPublicResponse(h.baseURL))
	}

	c.JSON(http.StatusOK, gin.H{"mentors": publicMentors})
}

func (h *MentorHandler) GetPublicMentorByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		respondError(c, http.StatusBadRequest, "Invalid ID", fmt.Errorf("invalid mentor id %q: %w", idStr, err))
		return
	}

	mentor, err := h.service.GetMentorByID(c.Request.Context(), id, models.FilterOptions{OnlyVisible: true})
	if err != nil {
		respondError(c, http.StatusNotFound, "Mentor not found", fmt.Errorf("mentor id=%d not found: %w", id, err))
		return
	}

	publicMentor := mentor.ToPublicResponse(h.baseURL)
	c.JSON(http.StatusOK, publicMentor)
}

func (h *MentorHandler) GetInternalMentors(c *gin.Context) {
	forceRefresh := c.Query("force_reset_cache") == "true"
	id := c.Query("id")
	slug := c.Query("slug")
	rec := c.Query("rec")

	var body struct {
		OnlyVisible    bool `json:"only_visible"`
		ShowHidden     bool `json:"show_hidden"`
		DropLongFields bool `json:"drop_long_fields"`
	}
	_ = c.ShouldBindJSON(&body) //nolint:errcheck // Optional body parameters, errors are not critical

	opts := models.FilterOptions{
		OnlyVisible:    body.OnlyVisible,
		ShowHidden:     body.ShowHidden,
		DropLongFields: body.DropLongFields,
		ForceRefresh:   forceRefresh,
	}

	if id != "" {
		mentorID, err := strconv.Atoi(id)
		if err != nil {
			respondError(c, http.StatusBadRequest, "Invalid ID", fmt.Errorf("invalid mentor id %q: %w", id, err))
			return
		}
		mentor, err := h.service.GetMentorByID(c.Request.Context(), mentorID, opts)
		if err != nil {
			respondError(c, http.StatusNotFound, "Mentor not found", fmt.Errorf("mentor id=%d not found: %w", mentorID, err))
			return
		}
		c.JSON(http.StatusOK, mentor)
		return
	}

	if slug != "" {
		mentor, err := h.service.GetMentorBySlug(c.Request.Context(), slug, opts)
		if err != nil {
			respondError(c, http.StatusNotFound, "Mentor not found", fmt.Errorf("mentor slug=%q not found: %w", slug, err))
			return
		}
		c.JSON(http.StatusOK, mentor)
		return
	}

	if rec != "" {
		mentor, err := h.service.GetMentorByMentorId(c.Request.Context(), rec, opts)
		if err != nil {
			respondError(c, http.StatusNotFound, "Mentor not found", fmt.Errorf("mentor rec=%q not found: %w", rec, err))
			return
		}
		c.JSON(http.StatusOK, mentor)
		return
	}

	mentors, err := h.service.GetAllMentors(c.Request.Context(), opts)
	if err != nil {
		logger.Error("Failed to fetch mentors in GetInternalMentors",
			zap.Error(err),
			zap.Bool("only_visible", opts.OnlyVisible),
			zap.Bool("force_refresh", opts.ForceRefresh))
		respondError(c, http.StatusInternalServerError, "Failed to fetch mentors", err)
		return
	}

	c.JSON(http.StatusOK, mentors)
}
