package pingcode

// WorkItemKind is a domain work-item category used by the CLI.
type WorkItemKind string

const (
	KindBug         WorkItemKind = "bug"
	KindRequirement WorkItemKind = "requirement"
)

// PageResponse mirrors the MCP-validated PingCode pagination payload.
type PageResponse[T any] struct {
	PageSize  int `json:"page_size"`
	PageIndex int `json:"page_index"`
	Total     int `json:"total"`
	Values    []T `json:"values"`
}

// Ref is a lightweight PingCode object reference.
type Ref struct {
	ID          string `json:"id"`
	URL         string `json:"url,omitempty"`
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Type        string `json:"type,omitempty"`
	Color       string `json:"color,omitempty"`
}

// Project is a PingCode project.
type Project struct {
	ID         string `json:"id"`
	URL        string `json:"url,omitempty"`
	Name       string `json:"name"`
	Identifier string `json:"identifier,omitempty"`
	Type       string `json:"type,omitempty"`
}

// Team is enterprise team metadata.
type Team struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// User is a PingCode user.
type User struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
}

// WorkItemType describes a project work item type.
type WorkItemType struct {
	ID    string `json:"id"`
	URL   string `json:"url,omitempty"`
	Name  string `json:"name"`
	Group string `json:"group,omitempty"`
}

// WorkItemState describes a workflow state.
type WorkItemState struct {
	ID    string `json:"id"`
	URL   string `json:"url,omitempty"`
	Name  string `json:"name"`
	Type  string `json:"type,omitempty"`
	Color string `json:"color,omitempty"`
}

// WorkItemPriority describes a priority option.
type WorkItemPriority struct {
	ID   string `json:"id"`
	URL  string `json:"url,omitempty"`
	Name string `json:"name"`
}

// ProjectMember is a project membership entry.
type ProjectMember struct {
	ID          string `json:"id"`
	Type        string `json:"type,omitempty"`
	User        *Ref   `json:"user,omitempty"`
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// Comment is a work-item comment.
type Comment struct {
	ID        string `json:"id"`
	URL       string `json:"url,omitempty"`
	Content   string `json:"content,omitempty"`
	CreatedAt int64  `json:"created_at,omitempty"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
	CreatedBy *Ref   `json:"created_by,omitempty"`
	UpdatedBy *Ref   `json:"updated_by,omitempty"`
}

// WorkItem is the core domain object.
type WorkItem struct {
	ID               string         `json:"id"`
	URL              string         `json:"url,omitempty"`
	HTMLURL          string         `json:"html_url,omitempty"`
	Identifier       string         `json:"identifier,omitempty"`
	Title            string         `json:"title,omitempty"`
	Description      string         `json:"description,omitempty"`
	Type             *Ref           `json:"type,omitempty"`
	State            *Ref           `json:"state,omitempty"`
	Priority         *Ref           `json:"priority,omitempty"`
	Assignee         *Ref           `json:"assignee,omitempty"`
	Parent           *Ref           `json:"parent,omitempty"`
	CreatedAt        int64          `json:"created_at,omitempty"`
	UpdatedAt        int64          `json:"updated_at,omitempty"`
	Properties       map[string]any `json:"properties,omitempty"`
	PublicImageToken *string        `json:"public_image_token,omitempty"`
}

// WorkItemPayload is the create request body (omitempty is safe for create).
type WorkItemPayload struct {
	ProjectID   string         `json:"project_id,omitempty"`
	TypeID      string         `json:"type_id,omitempty"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	StateID     string         `json:"state_id,omitempty"`
	PriorityID  string         `json:"priority_id,omitempty"`
	AssigneeID  string         `json:"assignee_id,omitempty"`
	ParentID    string         `json:"parent_id,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}

// WorkItemPatch is a sparse PATCH body.
// Present keys are sent to the API; nil values serialize as JSON null (clear).
type WorkItemPatch map[string]any

// Set records a concrete patch value.
func (p WorkItemPatch) Set(key string, value any) {
	p[key] = value
}

// Clear records an explicit JSON null clear.
func (p WorkItemPatch) Clear(key string) {
	p[key] = nil
}

// WorkItemStatePlan is a project workflow plan.
type WorkItemStatePlan struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	ProjectType  string `json:"project_type,omitempty"`
	WorkItemType string `json:"work_item_type,omitempty"`
}

// WorkItemStateFlow is a legal transition edge.
type WorkItemStateFlow struct {
	ID          string `json:"id,omitempty"`
	FromStateID string `json:"from_state_id,omitempty"`
	ToStateID   string `json:"to_state_id,omitempty"`
	ToState     *Ref   `json:"to_state,omitempty"`
}

// WorkItemListQuery carries list/search filters.
type WorkItemListQuery struct {
	Identifier              string
	ProjectIDs              string
	TypeIDs                 string
	StateIDs                string
	AssigneeIDs             string
	PriorityIDs             string
	ParentIDs               string
	Keywords                string
	UpdatedBetween          string
	IncludeDeleted          *bool
	IncludeArchived         *bool
	IncludePublicImageToken *bool
	PageIndex               *int
	PageSize                *int
}

// TokenResponse is the /v1/auth/token payload.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
}

// SchemaContext is the resolved project schema for one kind.
type SchemaContext struct {
	Project    Project
	Type       WorkItemType
	Types      []WorkItemType
	States     []WorkItemState
	Priorities []WorkItemPriority
	Members    []ProjectMember
}

// FieldChange records one planned field mutation.
type FieldChange struct {
	Field string `json:"field"`
	From  any    `json:"from"`
	To    any    `json:"to"`
}

// WorkItemSummary is the agent-facing work item projection.
type WorkItemSummary struct {
	ID           string   `json:"id"`
	Identifier   string   `json:"identifier,omitempty"`
	Title        string   `json:"title,omitempty"`
	State        string   `json:"state,omitempty"`
	Priority     string   `json:"priority,omitempty"`
	Assignee     string   `json:"assignee,omitempty"`
	ImageCount   int      `json:"imageCount"`
	ImageSources []string `json:"imageSources,omitempty"`
	URL          string   `json:"url,omitempty"`
}

// WorkItemDetail extends summary with description metadata.
type WorkItemDetail struct {
	WorkItemSummary
	Description string         `json:"description,omitempty"`
	CreatedAt   int64          `json:"createdAt,omitempty"`
	UpdatedAt   int64          `json:"updatedAt,omitempty"`
	Parent      map[string]any `json:"parent,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}
