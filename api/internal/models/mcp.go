package models

// MCPRequest represents a JSON-RPC 2.0 request
type MCPRequest struct {
	JSONRPC string                 `json:"jsonrpc"` // Must be "2.0"
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
	ID      interface{}            `json:"id"` // Can be string, number, or null
}

// MCPResponse represents a JSON-RPC 2.0 response
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"` // Must be "2.0"
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

// MCPError represents a JSON-RPC 2.0 error
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Standard JSON-RPC error codes
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// MCPTool represents a tool definition following MCP protocol
type MCPTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ListMentorsParams represents parameters for the list_mentors tool
type ListMentorsParams struct {
	Tags       []string `json:"tags,omitempty"`       // Filter by tags
	Experience string   `json:"experience,omitempty"` // Filter by experience level
	MinPrice   string   `json:"minPrice,omitempty"`   // Minimum price (inclusive)
	MaxPrice   string   `json:"maxPrice,omitempty"`   // Maximum price (inclusive)
	Workplace  string   `json:"workplace,omitempty"`  // Filter by workplace
	Limit      int      `json:"limit,omitempty"`      // Limit results (default: 50, max: 200)
}

// GetMentorParams represents parameters for the get_mentor tool
type GetMentorParams struct {
	ID   *int    `json:"id,omitempty"`   // Mentor ID
	Slug *string `json:"slug,omitempty"` // Mentor slug
}

// SearchMentorsParams represents parameters for the search_mentors tool
type SearchMentorsParams struct {
	Query      string   `json:"query"`                // Search keywords (space-separated)
	Tags       []string `json:"tags,omitempty"`       // Filter by tags
	Experience string   `json:"experience,omitempty"` // Filter by experience level
	MinPrice   string   `json:"minPrice,omitempty"`   // Minimum price (inclusive)
	MaxPrice   string   `json:"maxPrice,omitempty"`   // Maximum price (inclusive)
	Workplace  string   `json:"workplace,omitempty"`  // Filter by workplace
	Limit      int      `json:"limit,omitempty"`      // Limit results (default: 20, max: 100)
}

// MCPMentorBasic represents basic mentor information for list_mentors tool
type MCPMentorBasic struct {
	ID           int      `json:"id"`
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	JobTitle     string   `json:"jobTitle"`
	Workplace    string   `json:"workplace"`
	Experience   string   `json:"experience"`
	Tags         []string `json:"tags"`
	Competencies string   `json:"competencies"`
	Price        string   `json:"price"`
	DoneSessions int      `json:"doneSessions"`
	MentorURL    string   `json:"mentorUrl"`
}

// MCPMentorExtended represents extended mentor information for get_mentor and search results
type MCPMentorExtended struct {
	ID           int      `json:"id"`
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	JobTitle     string   `json:"jobTitle"`
	Workplace    string   `json:"workplace"`
	Experience   string   `json:"experience"`
	Tags         []string `json:"tags"`
	Competencies string   `json:"competencies"`
	Price        string   `json:"price"`
	DoneSessions int      `json:"doneSessions"`
	Description  string   `json:"description"`
	About        string   `json:"about"`
	MentorURL    string   `json:"mentorUrl"`
}

// ListMentorsResult represents the result of list_mentors tool invocation
type ListMentorsResult struct {
	Mentors []MCPMentorBasic `json:"mentors"`
	Count   int              `json:"count"`
}

// GetMentorResult represents the result of get_mentor tool invocation
type GetMentorResult struct {
	Mentor *MCPMentorExtended `json:"mentor"`
}

// SearchMentorsResult represents the result of search_mentors tool invocation
type SearchMentorsResult struct {
	Mentors []MCPMentorExtended `json:"mentors"`
	Count   int                 `json:"count"`
}

// ToMCPBasic converts a Mentor to MCPMentorBasic format
func (m *Mentor) ToMCPBasic(baseURL string) MCPMentorBasic {
	return MCPMentorBasic{
		ID:           m.LegacyID,
		Slug:         m.Slug,
		Name:         m.Name,
		JobTitle:     m.Job,
		Workplace:    m.Workplace,
		Experience:   m.Experience,
		Tags:         m.Tags,
		Competencies: m.Competencies,
		Price:        m.Price,
		DoneSessions: m.MenteeCount,
		MentorURL:    baseURL + "/mentor/" + m.Slug,
	}
}

// ToMCPExtended converts a Mentor to MCPMentorExtended format
func (m *Mentor) ToMCPExtended(baseURL string) MCPMentorExtended {
	return MCPMentorExtended{
		ID:           m.LegacyID,
		Slug:         m.Slug,
		Name:         m.Name,
		JobTitle:     m.Job,
		Workplace:    m.Workplace,
		Experience:   m.Experience,
		Tags:         m.Tags,
		Competencies: m.Competencies,
		Price:        m.Price,
		DoneSessions: m.MenteeCount,
		Description:  m.Description,
		About:        m.About,
		MentorURL:    baseURL + "/mentor/" + m.Slug,
	}
}
