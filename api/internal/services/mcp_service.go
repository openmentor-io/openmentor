package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"go.uber.org/zap"
)

// MCPService handles MCP (Model Context Protocol) operations for mentor search
type MCPService struct {
	repo    *repository.MentorRepository
	baseURL string
}

// NewMCPService creates a new MCP service instance
func NewMCPService(repo *repository.MentorRepository, baseURL string) *MCPService {
	return &MCPService{
		repo:    repo,
		baseURL: baseURL,
	}
}

// ListMentors returns all active mentors with optional filtering
func (s *MCPService) ListMentors(ctx context.Context, params *models.ListMentorsParams) (*models.ListMentorsResult, error) {
	// Set default limit
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > 200 {
		params.Limit = 200
	}

	// Fetch all visible mentors
	opts := models.FilterOptions{
		OnlyVisible:    true,
		ShowHidden:     false,
		DropLongFields: true, // Drop About and Description for list view
		ForceRefresh:   false,
	}

	mentors, err := s.repo.GetAll(ctx, opts)
	if err != nil {
		logger.Error("Failed to fetch mentors for MCP list", zap.Error(err))
		return nil, err
	}

	// Apply filters
	filtered := s.filterMentors(mentors, params.Tags, params.Experience, params.MinPrice, params.MaxPrice, params.Workplace)

	// Apply limit
	if len(filtered) > params.Limit {
		filtered = filtered[:params.Limit]
	}

	// Convert to MCP basic response
	result := make([]models.MCPMentorBasic, 0, len(filtered))
	for _, mentor := range filtered {
		result = append(result, mentor.ToMCPBasic(s.baseURL))
	}

	return &models.ListMentorsResult{
		Mentors: result,
		Count:   len(result),
	}, nil
}

// GetMentor returns extended information for a specific mentor
func (s *MCPService) GetMentor(ctx context.Context, params *models.GetMentorParams) (*models.GetMentorResult, error) {
	if params.ID == nil && params.Slug == nil {
		return nil, fmt.Errorf("either id or slug must be provided")
	}

	opts := models.FilterOptions{
		OnlyVisible:    true,
		ShowHidden:     false,
		DropLongFields: false, // Include full info
		ForceRefresh:   false,
	}

	var mentor *models.Mentor
	var err error

	if params.ID != nil {
		mentor, err = s.repo.GetByID(ctx, *params.ID, opts)
	} else {
		mentor, err = s.repo.GetBySlug(ctx, *params.Slug, opts)
	}

	if err != nil {
		logger.Error("Failed to fetch mentor for MCP get",
			zap.Any("id", params.ID),
			zap.Any("slug", params.Slug),
			zap.Error(err))
		return nil, err
	}

	if mentor == nil {
		return &models.GetMentorResult{Mentor: nil}, nil
	}

	extended := mentor.ToMCPExtended(s.baseURL)
	return &models.GetMentorResult{Mentor: &extended}, nil
}

// SearchMentors performs keyword search with optional filtering
func (s *MCPService) SearchMentors(ctx context.Context, params *models.SearchMentorsParams) (*models.SearchMentorsResult, error) {
	if params.Query == "" {
		return nil, fmt.Errorf("query parameter is required")
	}

	// Set default limit
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	// Fetch all visible mentors with full info
	opts := models.FilterOptions{
		OnlyVisible:    true,
		ShowHidden:     false,
		DropLongFields: false, // Need full info for search
		ForceRefresh:   false,
	}

	mentors, err := s.repo.GetAll(ctx, opts)
	if err != nil {
		logger.Error("Failed to fetch mentors for MCP search", zap.Error(err))
		return nil, err
	}

	// Apply filters first
	filtered := s.filterMentors(mentors, params.Tags, params.Experience, params.MinPrice, params.MaxPrice, params.Workplace)

	// Apply keyword search
	keywords := s.parseKeywords(params.Query)
	searched := s.searchMentors(filtered, keywords)

	// Apply limit
	if len(searched) > params.Limit {
		searched = searched[:params.Limit]
	}

	// Convert to MCP extended response
	result := make([]models.MCPMentorExtended, 0, len(searched))
	for _, mentor := range searched {
		result = append(result, mentor.ToMCPExtended(s.baseURL))
	}

	return &models.SearchMentorsResult{
		Mentors: result,
		Count:   len(result),
	}, nil
}

// GetAvailableTools returns the MCP tool definitions
func (s *MCPService) GetAvailableTools() []models.MCPTool {
	return []models.MCPTool{
		{
			Name:        "list_mentors",
			Description: "List all active mentors with optional filtering by tags, experience, price range, and workplace. Returns basic mentor information.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tags": map[string]interface{}{
						"type":        "array",
						"items":       map[string]string{"type": "string"},
						"description": "Filter by mentor tags (e.g., ['Python', 'Machine Learning'])",
					},
					"experience": map[string]interface{}{
						"type":        "string",
						"description": "Filter by experience level (e.g., 'Senior', 'Middle', 'Junior')",
					},
					"minPrice": map[string]interface{}{
						"type":        "string",
						"description": "Minimum price filter (inclusive)",
					},
					"maxPrice": map[string]interface{}{
						"type":        "string",
						"description": "Maximum price filter (inclusive)",
					},
					"workplace": map[string]interface{}{
						"type":        "string",
						"description": "Filter by workplace/company name",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (default: 50, max: 200)",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
		{
			Name:        "get_mentor",
			Description: "Get detailed information about a specific mentor by ID or slug. Returns extended mentor information including description and about sections.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "integer",
						"description": "Mentor ID",
					},
					"slug": map[string]interface{}{
						"type":        "string",
						"description": "Mentor slug (URL-friendly identifier)",
					},
				},
				"oneOf": []map[string]interface{}{
					{"required": []string{"id"}},
					{"required": []string{"slug"}},
				},
			},
		},
		{
			Name:        "search_mentors",
			Description: "Search for mentors by keywords in their competencies, description, and about sections. Supports additional filtering by tags, experience, price, and workplace. Returns extended mentor information.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search keywords (comma-separated). Searches in competencies, description, and about fields.",
					},
					"tags": map[string]interface{}{
						"type":        "array",
						"items":       map[string]string{"type": "string"},
						"description": "Filter by mentor tags",
					},
					"experience": map[string]interface{}{
						"type":        "string",
						"description": "Filter by experience level",
					},
					"minPrice": map[string]interface{}{
						"type":        "string",
						"description": "Minimum price filter (inclusive)",
					},
					"maxPrice": map[string]interface{}{
						"type":        "string",
						"description": "Maximum price filter (inclusive)",
					},
					"workplace": map[string]interface{}{
						"type":        "string",
						"description": "Filter by workplace/company name",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (default: 20, max: 100)",
						"default":     20,
						"minimum":     1,
						"maximum":     100,
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

// filterMentors applies filters to a list of mentors
func (s *MCPService) filterMentors(mentors []*models.Mentor, tags []string, experience, minPrice, maxPrice, workplace string) []*models.Mentor {
	filtered := make([]*models.Mentor, 0, len(mentors))

	for _, mentor := range mentors {
		// Filter by tags
		if len(tags) > 0 && !s.hasAnyTag(mentor.Tags, tags) {
			continue
		}

		// Filter by experience (case-insensitive partial match)
		if experience != "" && !strings.Contains(strings.ToLower(mentor.Experience), strings.ToLower(experience)) {
			continue
		}

		// Filter by price range
		if minPrice != "" && !s.priceInRange(mentor.Price, minPrice, true) {
			continue
		}
		if maxPrice != "" && !s.priceInRange(mentor.Price, maxPrice, false) {
			continue
		}

		// Filter by workplace (case-insensitive partial match)
		if workplace != "" && !strings.Contains(strings.ToLower(mentor.Workplace), strings.ToLower(workplace)) {
			continue
		}

		filtered = append(filtered, mentor)
	}

	return filtered
}

// hasAnyTag checks if mentor has any of the specified tags
func (s *MCPService) hasAnyTag(mentorTags, filterTags []string) bool {
	for _, filterTag := range filterTags {
		for _, mentorTag := range mentorTags {
			if strings.EqualFold(mentorTag, filterTag) {
				return true
			}
		}
	}
	return false
}

// priceInRange checks if mentor price is within range
// Simple string comparison - assumes consistent price format
func (s *MCPService) priceInRange(mentorPrice, comparePrice string, isMin bool) bool {
	mp, err := strconv.Atoi(mentorPrice)
	if err != nil {
		mp = 0
	}
	cp, err := strconv.Atoi(comparePrice)
	if err != nil {
		cp = 0
	}

	if isMin {
		return mp >= cp
	}

	return mp <= cp
}

// parseKeywords splits query into keywords
func (s *MCPService) parseKeywords(query string) []string {
	keywords := strings.Split(strings.ToLower(query), ",")
	// Remove duplicates
	seen := make(map[string]bool)
	unique := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		if !seen[keyword] && keyword != "" {
			seen[keyword] = true
			unique = append(unique, keyword)
		}
	}
	return unique
}

// searchMentors performs keyword search in competencies, description, and about fields
func (s *MCPService) searchMentors(mentors []*models.Mentor, keywords []string) []*models.Mentor {
	if len(keywords) == 0 {
		return mentors
	}

	result := make([]*models.Mentor, 0, len(mentors))

	for _, mentor := range mentors {
		// Create searchable text (lowercase)
		searchText := strings.ToLower(
			mentor.Competencies + " " +
				mentor.Description + " " +
				mentor.About,
		)

		// Check if any keyword matches
		matched := false
		for _, keyword := range keywords {
			if strings.Contains(searchText, keyword) {
				matched = true
				break
			}
		}

		if matched {
			result = append(result, mentor)
		}
	}

	return result
}

// ParseParams safely parses params from map to struct
func ParseParams(params map[string]interface{}, target interface{}) error {
	// Convert map to JSON
	jsonData, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal params: %w", err)
	}

	// Unmarshal into target struct
	if err := json.Unmarshal(jsonData, target); err != nil {
		return fmt.Errorf("failed to unmarshal params: %w", err)
	}

	return nil
}
