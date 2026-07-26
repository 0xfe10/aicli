package pingcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var typeNameCandidates = map[WorkItemKind][]string{
	KindBug:         {"缺陷", "bug", "BUG"},
	KindRequirement: {"需求", "用户故事", "story", "requirement"},
}

// Service owns domain workflows: name resolution, dry-run plans, and guards.
type Service struct {
	cfg    Config
	store  *AuthStore
	client *Client

	cachedAssignee string
}

// NewService constructs the domain service.
func NewService(cfg Config, store *AuthStore, client *Client) *Service {
	return &Service{cfg: cfg, store: store, client: client}
}

// GetSchema returns project schema, optionally scoped to one kind.
func (s *Service) GetSchema(ctx context.Context, kind *WorkItemKind, projectIdentifier, projectID, typeID string) (map[string]any, error) {
	project, err := s.client.ResolveProject(ctx, firstNonEmpty(projectIdentifier, s.cfg.ProjectIdentifier), firstNonEmpty(projectID, s.cfg.ProjectID))
	if err != nil {
		return nil, err
	}
	typesPage, err := s.client.ListWorkItemTypes(ctx, project.ID, 0, 100)
	if err != nil {
		return nil, err
	}
	prioritiesPage, err := s.client.ListWorkItemPriorities(ctx, project.ID, 0, 100)
	if err != nil {
		return nil, err
	}
	membersPage, err := s.client.ListProjectMembers(ctx, project.ID, 0, 200)
	if err != nil {
		return nil, err
	}
	if kind == nil {
		return map[string]any{
			"project":    project,
			"types":      typesPage.Values,
			"priorities": prioritiesPage.Values,
			"members":    membersPage.Values,
		}, nil
	}
	typ, err := s.resolveType(typesPage.Values, *kind, typeID)
	if err != nil {
		return nil, err
	}
	statesPage, err := s.client.ListWorkItemStates(ctx, project.ID, typ.ID, 0, 100)
	if err != nil {
		return nil, err
	}
	flows := []map[string]any{}
	if plans, planErr := s.client.GetWorkItemStatePlans(ctx, project.ID); planErr == nil {
		if plan := findStatePlan(plans, project, typ); plan != nil && len(statesPage.Values) > 0 {
			for _, st := range statesPage.Values {
				if edges, flowErr := s.client.GetWorkItemStateFlows(ctx, plan.ID, st.ID); flowErr == nil {
					flows = append(flows, map[string]any{
						"fromState": map[string]any{"id": st.ID, "name": st.Name},
						"to":        summarizeFlows(edges, statesPage.Values),
					})
				}
			}
		}
	}
	return map[string]any{
		"project":          project,
		"type":             typ,
		"types":            typesPage.Values,
		"states":           statesPage.Values,
		"priorities":       prioritiesPage.Values,
		"members":          membersPage.Values,
		"stateTransitions": flows,
	}, nil
}

// ListProjects lists projects with optional identifier filter.
func (s *Service) ListProjects(ctx context.Context, identifier string, pageIndex, pageSize int) (PageResponse[Project], error) {
	return s.client.ListProjects(ctx, identifier, pageIndex, pageSize)
}

// GetWorkItemDetail fetches a work item by id or identifier.
func (s *Service) GetWorkItemDetail(ctx context.Context, kind WorkItemKind, workItemID, identifier string, includeComments bool, projectIdentifier, projectID string) (map[string]any, error) {
	schema, err := s.getKindSchema(ctx, kind, projectIdentifier, projectID, "")
	if err != nil {
		return nil, err
	}
	item, err := s.resolveWorkItemStrict(ctx, workItemID, identifier, schema)
	if err != nil {
		return nil, err
	}
	detail := detailWorkItem(item, s.cfg.BaseURL, true)
	if !includeComments {
		return map[string]any{"target": detail}, nil
	}
	page, err := s.client.ListWorkItemComments(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"target": detail,
		"comments": map[string]any{
			"total":     page.Total,
			"pageIndex": page.PageIndex,
			"pageSize":  page.PageSize,
			"values":    page.Values,
		},
	}, nil
}

// SearchOptions configures work-item search.
type SearchOptions struct {
	Kinds             []WorkItemKind
	Keywords          string
	StateNames        []string
	PriorityNames     []string
	AssigneeNames     []string
	UpdatedAfter      string
	UpdatedBefore     string
	PageIndex         int
	PageSize          int
	ProjectIdentifier string
	ProjectID         string
}

// SearchWorkItems searches across kinds with dedupe and truncation metadata.
func (s *Service) SearchWorkItems(ctx context.Context, opt SearchOptions) (map[string]any, error) {
	if len(opt.Kinds) == 0 {
		opt.Kinds = []WorkItemKind{KindBug, KindRequirement}
	}
	updatedBetween, err := buildUpdatedBetween(opt.UpdatedAfter, opt.UpdatedBefore)
	if err != nil {
		return nil, err
	}
	byKind := []map[string]any{}
	values := []WorkItemSummary{}
	seen := map[string]struct{}{}
	for _, kind := range opt.Kinds {
		page, err := s.list(ctx, kind, listOptions{
			ProjectIdentifier: opt.ProjectIdentifier,
			ProjectID:         opt.ProjectID,
			Keywords:          opt.Keywords,
			StateNames:        opt.StateNames,
			PriorityNames:     opt.PriorityNames,
			AssigneeNames:     opt.AssigneeNames,
			UpdatedBetween:    updatedBetween,
			PageIndex:         opt.PageIndex,
			PageSize:          opt.PageSize,
		})
		if err != nil {
			return nil, err
		}
		hasMore := page.Total > (page.PageIndex+1)*page.PageSize
		byKind = append(byKind, map[string]any{
			"kind": kind, "total": page.Total, "pageIndex": page.PageIndex, "pageSize": page.PageSize, "hasMore": hasMore,
		})
		for _, item := range page.Values {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			values = append(values, summarizeWorkItem(item, s.cfg.BaseURL))
		}
	}
	total := 0
	truncated := false
	for _, entry := range byKind {
		total += entry["total"].(int)
		if entry["hasMore"].(bool) {
			truncated = true
		}
	}
	out := map[string]any{
		"total": total, "truncated": truncated, "byKind": byKind, "values": values,
	}
	if truncated {
		out["note"] = "结果超过一页，请用 pageIndex/pageSize 翻页或收紧过滤条件后重新查询。"
	}
	return out, nil
}

// MyWorkOptions configures the mine query.
type MyWorkOptions struct {
	AssigneeName      string
	Kinds             []WorkItemKind
	StateNames        []string
	UpdatedAfter      string
	UpdatedBefore     string
	PageSize          int
	ProjectIdentifier string
	ProjectID         string
}

// GetMyWork lists work items for the current user or default assignee.
func (s *Service) GetMyWork(ctx context.Context, opt MyWorkOptions) (map[string]any, error) {
	assignee, err := s.resolveCurrentAssigneeName(ctx, opt.AssigneeName)
	if err != nil {
		return nil, err
	}
	kinds := opt.Kinds
	if len(kinds) == 0 {
		kinds = []WorkItemKind{KindBug, KindRequirement}
	}
	updatedBetween, err := buildUpdatedBetween(opt.UpdatedAfter, opt.UpdatedBefore)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	items := []WorkItemSummary{}
	byKind := []map[string]any{}
	for _, kind := range kinds {
		page, err := s.list(ctx, kind, listOptions{
			AssigneeNames:     []string{assignee},
			StateNames:        opt.StateNames,
			UpdatedBetween:    updatedBetween,
			PageSize:          opt.PageSize,
			ProjectIdentifier: opt.ProjectIdentifier,
			ProjectID:         opt.ProjectID,
		})
		if err != nil {
			return nil, err
		}
		hasMore := page.Total > (page.PageIndex+1)*page.PageSize
		byKind = append(byKind, map[string]any{
			"kind": kind, "total": page.Total, "pageIndex": page.PageIndex, "pageSize": page.PageSize, "hasMore": hasMore,
		})
		for _, item := range page.Values {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			items = append(items, summarizeWorkItem(item, s.cfg.BaseURL))
		}
	}
	groupMap := map[string][]WorkItemSummary{}
	order := []string{}
	for _, item := range items {
		status := item.State
		if status == "" {
			status = "未分组"
		}
		if _, ok := groupMap[status]; !ok {
			order = append(order, status)
		}
		groupMap[status] = append(groupMap[status], item)
	}
	groups := make([]map[string]any, 0, len(order))
	for _, status := range order {
		groups = append(groups, map[string]any{
			"status": status,
			"count":  len(groupMap[status]),
			"items":  groupMap[status],
		})
	}
	truncated := false
	for _, entry := range byKind {
		if entry["hasMore"].(bool) {
			truncated = true
			break
		}
	}
	out := map[string]any{
		"assigneeName": assignee,
		"total":        len(items),
		"truncated":    truncated,
		"byKind":       byKind,
		"groups":       groups,
	}
	if truncated {
		out["note"] = "结果超过一页，请用 pageSize 调大或收紧 stateNames/时间范围后重新查询。"
	}
	return out, nil
}

// CreateInput is stdin JSON for work-item create.
type CreateInput struct {
	Kind              WorkItemKind   `json:"kind"`
	Title             string         `json:"title"`
	Description       string         `json:"description,omitempty"`
	PriorityName      string         `json:"priorityName,omitempty"`
	AssigneeName      string         `json:"assigneeName,omitempty"`
	StatusName        string         `json:"statusName,omitempty"`
	Parent            string         `json:"parent,omitempty"`
	Properties        map[string]any `json:"properties,omitempty"`
	ProjectIdentifier string         `json:"projectIdentifier,omitempty"`
	ProjectID         string         `json:"projectId,omitempty"`
}

// CreateWorkItem plans or applies creation. apply=false never writes.
func (s *Service) CreateWorkItem(ctx context.Context, in CreateInput, apply bool) (map[string]any, error) {
	if in.Kind == "" {
		in.Kind = KindBug
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, NewError(CodeInvalidInput, "title 必填")
	}
	schema, err := s.getKindSchema(ctx, in.Kind, in.ProjectIdentifier, in.ProjectID, "")
	if err != nil {
		return nil, err
	}
	payload := WorkItemPayload{
		ProjectID: schema.Project.ID,
		TypeID:    schema.Type.ID,
		Title:     in.Title,
	}
	if in.Description != "" {
		payload.Description = in.Description
	}
	if in.PriorityName != "" {
		p, err := s.resolveNamed(in.PriorityName, schema.Priorities, "优先级")
		if err != nil {
			return nil, err
		}
		payload.PriorityID = p.ID
	}
	if in.AssigneeName != "" {
		m, err := s.resolveMember(in.AssigneeName, schema.Members)
		if err != nil {
			return nil, err
		}
		payload.AssigneeID = m.ID
	}
	if in.Parent != "" {
		parentID, err := s.resolveParentID(ctx, in.Parent, schema.Project.ID)
		if err != nil {
			return nil, err
		}
		payload.ParentID = parentID
	}
	if in.StatusName != "" {
		st, err := s.resolveNamed(in.StatusName, schema.States, "状态")
		if err != nil {
			return nil, err
		}
		payload.StateID = st.ID
	}
	if in.Properties != nil {
		payload.Properties = in.Properties
	}
	plan := map[string]any{
		"project": map[string]any{"id": schema.Project.ID, "identifier": schema.Project.Identifier, "name": schema.Project.Name},
		"type":    map[string]any{"id": schema.Type.ID, "name": schema.Type.Name},
		"payload": payload,
	}
	out := map[string]any{"dryRun": !apply, "plan": plan}
	if !apply {
		return out, nil
	}
	if err := AssertWritable(s.cfg); err != nil {
		return nil, err
	}
	created, err := s.client.CreateWorkItem(ctx, payload)
	if err != nil {
		return nil, err
	}
	out["created"] = summarizeWorkItem(created, s.cfg.BaseURL)
	return out, nil
}

// UpdateInput is stdin JSON for field updates.
type UpdateInput struct {
	Kind                     WorkItemKind   `json:"kind"`
	WorkItemID               string         `json:"workItemId,omitempty"`
	Identifier               string         `json:"identifier,omitempty"`
	Title                    *string        `json:"title,omitempty"`
	Description              *string        `json:"description,omitempty"`
	PriorityName             *string        `json:"priorityName,omitempty"`
	AssigneeName             *string        `json:"assigneeName,omitempty"`
	Parent                   *string        `json:"parent,omitempty"`
	Properties               map[string]any `json:"properties,omitempty"`
	ExpectedCurrentStateName string         `json:"expectedCurrentState,omitempty"`
	ProjectIdentifier        string         `json:"projectIdentifier,omitempty"`
	ProjectID                string         `json:"projectId,omitempty"`
}

// UpdateWorkItemFields plans or applies field updates with expected-state and no-change guards.
func (s *Service) UpdateWorkItemFields(ctx context.Context, in UpdateInput, apply bool) (map[string]any, error) {
	if in.Kind == "" {
		in.Kind = KindBug
	}
	schema, err := s.getKindSchema(ctx, in.Kind, in.ProjectIdentifier, in.ProjectID, "")
	if err != nil {
		return nil, err
	}
	item, err := s.resolveWorkItemStrict(ctx, in.WorkItemID, in.Identifier, schema)
	if err != nil {
		return nil, err
	}
	target := summarizeWorkItem(item, s.cfg.BaseURL)
	expectedSatisfied, err := assertExpectedStatus(item, in.ExpectedCurrentStateName)
	if err != nil {
		return nil, err
	}
	payload, changes, err := s.buildFieldChanges(ctx, item, schema, in)
	if err != nil {
		return nil, err
	}
	noChange := len(changes) == 0
	out := map[string]any{
		"dryRun":            !apply,
		"target":            target,
		"payload":           payload,
		"changes":           changes,
		"noChange":          noChange,
		"expectedSatisfied": expectedSatisfied,
	}
	if !apply || noChange {
		return out, nil
	}
	if err := AssertWritable(s.cfg); err != nil {
		return nil, err
	}

	// Re-read immediately before write to close the concurrency window.
	fresh, err := s.client.GetWorkItem(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	if _, err := assertExpectedStatus(fresh, in.ExpectedCurrentStateName); err != nil {
		return nil, err
	}
	payload, changes, err = s.buildFieldChanges(ctx, fresh, schema, in)
	if err != nil {
		return nil, err
	}
	out["target"] = summarizeWorkItem(fresh, s.cfg.BaseURL)
	out["payload"] = payload
	out["changes"] = changes
	out["noChange"] = len(changes) == 0
	out["expectedSatisfied"] = true
	if len(changes) == 0 {
		out["updated"] = summarizeWorkItem(fresh, s.cfg.BaseURL)
		return out, nil
	}

	updated, err := s.client.UpdateWorkItem(ctx, fresh.ID, payload)
	if err != nil {
		return nil, err
	}
	out["updated"] = summarizeWorkItem(updated, s.cfg.BaseURL)
	return out, nil
}

// TransitionInput is stdin JSON for status transitions.
type TransitionInput struct {
	Kind                     WorkItemKind `json:"kind"`
	WorkItemID               string       `json:"workItemId,omitempty"`
	Identifier               string       `json:"identifier,omitempty"`
	StatusName               string       `json:"statusName,omitempty"`
	StateID                  string       `json:"stateId,omitempty"`
	ExpectedCurrentStateName string       `json:"expectedCurrentState,omitempty"`
	Comment                  string       `json:"comment,omitempty"`
	ProjectIdentifier        string       `json:"projectIdentifier,omitempty"`
	ProjectID                string       `json:"projectId,omitempty"`
}

// TransitionWorkItem plans or applies a status change with re-check before write.
func (s *Service) TransitionWorkItem(ctx context.Context, in TransitionInput, apply bool) (map[string]any, error) {
	if in.Kind == "" {
		in.Kind = KindBug
	}
	if in.StatusName == "" && in.StateID == "" {
		return nil, NewError(CodeInvalidInput, "必须提供 statusName 或 stateId")
	}
	schema, err := s.getKindSchema(ctx, in.Kind, in.ProjectIdentifier, in.ProjectID, "")
	if err != nil {
		return nil, err
	}
	item, err := s.resolveWorkItemStrict(ctx, in.WorkItemID, in.Identifier, schema)
	if err != nil {
		return nil, err
	}
	toStateID := in.StateID
	toStatus := in.StatusName
	if toStateID == "" {
		st, err := s.resolveNamed(in.StatusName, schema.States, "状态")
		if err != nil {
			return nil, err
		}
		toStateID = st.ID
		toStatus = st.Name
	} else {
		for _, st := range schema.States {
			if st.ID == toStateID {
				toStatus = st.Name
				break
			}
		}
	}
	workflow, _ := s.resolveLegalTransitions(ctx, schema, refID(item.State), toStateID)
	plan := map[string]any{
		"target":               summarizeWorkItem(item, s.cfg.BaseURL),
		"currentStatus":        refName(item.State),
		"currentStateId":       refID(item.State),
		"toStatus":             toStatus,
		"toStateId":            toStateID,
		"availableStates":      mapStates(schema.States),
		"allowedTransitions":   workflow["allowedTransitions"],
		"transitionAllowed":    workflow["transitionAllowed"],
		"expectedCurrentState": in.ExpectedCurrentStateName,
		"willChange":           toStateID != refID(item.State),
		"note":                 workflow["note"],
	}
	out := map[string]any{"dryRun": !apply, "plan": plan}
	if !apply {
		return out, nil
	}
	// Re-check current state immediately before write.
	fresh, err := s.client.GetWorkItem(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	if _, err := assertExpectedStatus(fresh, in.ExpectedCurrentStateName); err != nil {
		return nil, err
	}
	if toStateID == refID(fresh.State) {
		out["noChange"] = true
		out["updated"] = summarizeWorkItem(fresh, s.cfg.BaseURL)
		return out, nil
	}
	if err := AssertWritable(s.cfg); err != nil {
		return nil, err
	}
	updated, err := s.client.UpdateWorkItem(ctx, fresh.ID, WorkItemPatch{"state_id": toStateID})
	if err != nil {
		return nil, err
	}
	out["updated"] = summarizeWorkItem(updated, s.cfg.BaseURL)
	if in.Comment != "" {
		comment, err := s.client.CreateWorkItemComment(ctx, updated.ID, in.Comment)
		if err != nil {
			return nil, err
		}
		out["comment"] = comment
	}
	return out, nil
}

// CommentInput is stdin JSON for comments.
type CommentInput struct {
	Kind              WorkItemKind `json:"kind"`
	WorkItemID        string       `json:"workItemId,omitempty"`
	Identifier        string       `json:"identifier,omitempty"`
	Content           string       `json:"content"`
	ProjectIdentifier string       `json:"projectIdentifier,omitempty"`
	ProjectID         string       `json:"projectId,omitempty"`
}

// AddComment plans or applies a single non-retried comment write.
func (s *Service) AddComment(ctx context.Context, in CommentInput, apply bool) (map[string]any, error) {
	if in.Kind == "" {
		in.Kind = KindBug
	}
	if strings.TrimSpace(in.Content) == "" {
		return nil, NewError(CodeInvalidInput, "content 必填")
	}
	schema, err := s.getKindSchema(ctx, in.Kind, in.ProjectIdentifier, in.ProjectID, "")
	if err != nil {
		return nil, err
	}
	item, err := s.resolveWorkItemStrict(ctx, in.WorkItemID, in.Identifier, schema)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"dryRun":  !apply,
		"target":  summarizeWorkItem(item, s.cfg.BaseURL),
		"content": in.Content,
	}
	if !apply {
		return out, nil
	}
	if err := AssertWritable(s.cfg); err != nil {
		return nil, err
	}
	// Comments are non-idempotent: never auto-retry. Use a dedicated one-shot request path.
	comment, err := s.client.CreateWorkItemComment(ctx, item.ID, in.Content)
	if err != nil {
		return nil, err
	}
	out["comment"] = comment
	return out, nil
}

type listOptions struct {
	ProjectIdentifier string
	ProjectID         string
	Keywords          string
	StateNames        []string
	PriorityNames     []string
	AssigneeNames     []string
	UpdatedBetween    string
	PageIndex         int
	PageSize          int
}

func (s *Service) list(ctx context.Context, kind WorkItemKind, opt listOptions) (PageResponse[WorkItem], error) {
	schema, err := s.getKindSchema(ctx, kind, opt.ProjectIdentifier, opt.ProjectID, "")
	if err != nil {
		return PageResponse[WorkItem]{}, err
	}
	stateIDs, err := s.namesToIDs(opt.StateNames, schema.States, "状态")
	if err != nil {
		return PageResponse[WorkItem]{}, err
	}
	priorityIDs, err := s.namesToIDs(opt.PriorityNames, schema.Priorities, "优先级")
	if err != nil {
		return PageResponse[WorkItem]{}, err
	}
	assigneeIDs, err := s.memberNamesToIDs(opt.AssigneeNames, schema.Members)
	if err != nil {
		return PageResponse[WorkItem]{}, err
	}
	pageIndex := opt.PageIndex
	pageSize := opt.PageSize
	if pageSize <= 0 {
		pageSize = 30
	}
	return s.client.ListWorkItems(ctx, WorkItemListQuery{
		ProjectIDs:     joinCap([]string{schema.Project.ID}),
		TypeIDs:        joinCap([]string{schema.Type.ID}),
		StateIDs:       joinCap(stateIDs),
		PriorityIDs:    joinCap(priorityIDs),
		AssigneeIDs:    joinCap(assigneeIDs),
		Keywords:       opt.Keywords,
		UpdatedBetween: opt.UpdatedBetween,
		PageIndex:      &pageIndex,
		PageSize:       &pageSize,
	})
}

func (s *Service) getKindSchema(ctx context.Context, kind WorkItemKind, projectIdentifier, projectID, typeID string) (SchemaContext, error) {
	project, err := s.client.ResolveProject(ctx, firstNonEmpty(projectIdentifier, s.cfg.ProjectIdentifier), firstNonEmpty(projectID, s.cfg.ProjectID))
	if err != nil {
		return SchemaContext{}, err
	}
	typesPage, err := s.client.ListWorkItemTypes(ctx, project.ID, 0, 100)
	if err != nil {
		return SchemaContext{}, err
	}
	typ, err := s.resolveType(typesPage.Values, kind, typeID)
	if err != nil {
		return SchemaContext{}, err
	}
	statesPage, err := s.client.ListWorkItemStates(ctx, project.ID, typ.ID, 0, 100)
	if err != nil {
		return SchemaContext{}, err
	}
	prioritiesPage, err := s.client.ListWorkItemPriorities(ctx, project.ID, 0, 100)
	if err != nil {
		return SchemaContext{}, err
	}
	membersPage, err := s.client.ListProjectMembers(ctx, project.ID, 0, 200)
	if err != nil {
		return SchemaContext{}, err
	}
	return SchemaContext{
		Project:    project,
		Type:       typ,
		Types:      typesPage.Values,
		States:     statesPage.Values,
		Priorities: prioritiesPage.Values,
		Members:    membersPage.Values,
	}, nil
}

func (s *Service) resolveCurrentAssigneeName(ctx context.Context, override string) (string, error) {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed, nil
	}
	if s.store != nil && s.store.HasToken() {
		if s.cachedAssignee != "" {
			return s.cachedAssignee, nil
		}
		user, err := s.client.GetCurrentUser(ctx)
		if err == nil {
			name := firstNonEmpty(user.DisplayName, user.Name)
			if name != "" {
				s.cachedAssignee = name
				return name, nil
			}
		}
	}
	if s.cfg.DefaultAssigneeName != "" {
		return s.cfg.DefaultAssigneeName, nil
	}
	return "", NewError(CodeConfigMissing, "缺少负责人。请先 pingcode auth login，或设置 PINGCODE_DEFAULT_ASSIGNEE_NAME，或传入 assigneeName。")
}

func (s *Service) resolveType(types []WorkItemType, kind WorkItemKind, explicitTypeID string) (WorkItemType, error) {
	configured := explicitTypeID
	if configured == "" {
		if kind == KindBug {
			configured = s.cfg.BugTypeID
		} else {
			configured = s.cfg.RequirementTypeID
		}
	}
	if configured != "" {
		for _, t := range types {
			if t.ID == configured {
				return t, nil
			}
		}
		// MCP falls back to treating configured id as opaque.
		if configured != "bug" || kind != KindBug {
			return WorkItemType{ID: configured, Name: configured}, nil
		}
	}
	candidates := typeNameCandidates[kind]
	for _, t := range types {
		n := normalizeName(t.Name)
		id := normalizeName(t.ID)
		for _, c := range candidates {
			if n == normalizeName(c) || id == normalizeName(c) {
				return t, nil
			}
		}
	}
	parts := make([]string, 0, len(types))
	for _, t := range types {
		parts = append(parts, fmt.Sprintf("%s(%s)", t.Name, t.ID))
	}
	return WorkItemType{}, NewError(CodeNotFound, fmt.Sprintf("未找到 %s 工作项类型，可用类型：%s", kind, strings.Join(parts, ", ")))
}

func (s *Service) resolveNamedState(name string, values []WorkItemState, label string) (WorkItemState, error) {
	matches := []WorkItemState{}
	norm := normalizeName(name)
	for _, item := range values {
		if normalizeName(item.Name) == norm || normalizeName(item.ID) == norm {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		names := make([]string, 0, len(values))
		for _, item := range values {
			names = append(names, item.Name)
		}
		return WorkItemState{}, NewError(CodeNotFound, fmt.Sprintf("未找到%s：%s，可用值：%s", label, name, strings.Join(names, ", ")))
	}
	if len(matches) > 1 {
		return WorkItemState{}, NewError(CodeAmbiguousName, fmt.Sprintf("%s名称存在歧义：%s", label, name))
	}
	return matches[0], nil
}

func (s *Service) resolveNamedPriority(name string, values []WorkItemPriority, label string) (WorkItemPriority, error) {
	matches := []WorkItemPriority{}
	norm := normalizeName(name)
	for _, item := range values {
		if normalizeName(item.Name) == norm || normalizeName(item.ID) == norm {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		names := make([]string, 0, len(values))
		for _, item := range values {
			names = append(names, item.Name)
		}
		return WorkItemPriority{}, NewError(CodeNotFound, fmt.Sprintf("未找到%s：%s，可用值：%s", label, name, strings.Join(names, ", ")))
	}
	if len(matches) > 1 {
		return WorkItemPriority{}, NewError(CodeAmbiguousName, fmt.Sprintf("%s名称存在歧义：%s", label, name))
	}
	return matches[0], nil
}

func (s *Service) resolveNamed(name string, values any, label string) (struct{ ID, Name string }, error) {
	switch v := values.(type) {
	case []WorkItemState:
		item, err := s.resolveNamedState(name, v, label)
		return struct{ ID, Name string }{item.ID, item.Name}, err
	case []WorkItemPriority:
		item, err := s.resolveNamedPriority(name, v, label)
		return struct{ ID, Name string }{item.ID, item.Name}, err
	default:
		return struct{ ID, Name string }{}, NewError(CodeInternalError, "unsupported named values")
	}
}

func (s *Service) resolveMember(name string, members []ProjectMember) (ProjectMember, error) {
	norm := normalizeName(name)
	matches := []ProjectMember{}
	for _, item := range members {
		candidates := []string{item.ID, item.Name, item.DisplayName}
		if item.User != nil {
			candidates = append(candidates, item.User.ID, item.User.Name, item.User.DisplayName)
		}
		for _, c := range candidates {
			if c != "" && normalizeName(c) == norm {
				m := item
				if m.User != nil && m.User.ID != "" {
					m.ID = m.User.ID
				}
				matches = append(matches, m)
				break
			}
		}
	}
	if len(matches) == 0 {
		available := make([]string, 0, len(members))
		for _, item := range members {
			label := item.ID
			if item.User != nil {
				label = firstNonEmpty(item.User.DisplayName, item.User.Name, item.DisplayName, item.Name, item.ID)
			} else {
				label = firstNonEmpty(item.DisplayName, item.Name, item.ID)
			}
			available = append(available, label)
		}
		return ProjectMember{}, NewError(CodeNotFound, fmt.Sprintf("未找到负责人：%s，可用成员：%s", name, strings.Join(available, ", ")))
	}
	if len(matches) > 1 {
		return ProjectMember{}, NewError(CodeAmbiguousName, fmt.Sprintf("负责人名称存在歧义：%s", name))
	}
	return matches[0], nil
}

func (s *Service) resolveParentID(ctx context.Context, parent, projectID string) (string, error) {
	if !strings.Contains(parent, "-") {
		return parent, nil
	}
	item, err := s.findByIdentifier(ctx, parent, projectID, "")
	if err != nil {
		return "", err
	}
	if item == nil {
		return "", NewError(CodeNotFound, fmt.Sprintf("未找到父工作项：%s", parent))
	}
	return item.ID, nil
}

func (s *Service) findByIdentifier(ctx context.Context, identifier, projectID, typeID string) (*WorkItem, error) {
	pageIndex, pageSize := 0, 1
	page, err := s.client.ListWorkItems(ctx, WorkItemListQuery{
		Identifier: identifier,
		ProjectIDs: projectID,
		TypeIDs:    typeID,
		PageIndex:  &pageIndex,
		PageSize:   &pageSize,
	})
	if err != nil {
		return nil, err
	}
	if len(page.Values) == 0 {
		return nil, nil
	}
	item := page.Values[0]
	return &item, nil
}

func (s *Service) resolveWorkItemStrict(ctx context.Context, workItemID, identifier string, schema SchemaContext) (WorkItem, error) {
	if workItemID != "" {
		item, err := s.client.GetWorkItem(ctx, workItemID)
		if err != nil {
			return WorkItem{}, err
		}
		if item.ID == "" {
			return WorkItem{}, NewError(CodeNotFound, fmt.Sprintf("未找到工作项：%s", workItemID))
		}
		return item, nil
	}
	if identifier == "" {
		return WorkItem{}, NewError(CodeInvalidInput, "必须提供 --id 或 --identifier")
	}
	item, err := s.findByIdentifier(ctx, identifier, schema.Project.ID, schema.Type.ID)
	if err != nil {
		return WorkItem{}, err
	}
	if item == nil {
		return WorkItem{}, NewError(CodeNotFound, fmt.Sprintf("未找到工作项：%s", identifier))
	}
	return *item, nil
}

func (s *Service) namesToIDs(names []string, values any, label string) ([]string, error) {
	switch v := values.(type) {
	case []WorkItemState:
		out := make([]string, 0, len(names))
		for _, name := range names {
			item, err := s.resolveNamedState(name, v, label)
			if err != nil {
				return nil, err
			}
			out = append(out, item.ID)
		}
		return out, nil
	case []WorkItemPriority:
		out := make([]string, 0, len(names))
		for _, name := range names {
			item, err := s.resolveNamedPriority(name, v, label)
			if err != nil {
				return nil, err
			}
			out = append(out, item.ID)
		}
		return out, nil
	default:
		return nil, NewError(CodeInternalError, "unsupported namesToIDs values")
	}
}

func (s *Service) memberNamesToIDs(names []string, members []ProjectMember) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, name := range names {
		m, err := s.resolveMember(name, members)
		if err != nil {
			return nil, err
		}
		out = append(out, m.ID)
	}
	return out, nil
}

func (s *Service) buildFieldChanges(ctx context.Context, item WorkItem, schema SchemaContext, in UpdateInput) (WorkItemPatch, []FieldChange, error) {
	payload := WorkItemPatch{}
	changes := []FieldChange{}
	if in.Title != nil && *in.Title != item.Title {
		payload.Set("title", *in.Title)
		changes = append(changes, FieldChange{Field: "title", From: item.Title, To: *in.Title})
	}
	if in.Description != nil && *in.Description != item.Description {
		// Empty string is an explicit clear/set-empty; serialize as "" (not omitted).
		payload.Set("description", *in.Description)
		changes = append(changes, FieldChange{Field: "description", From: item.Description, To: *in.Description})
	}
	if in.PriorityName != nil {
		if strings.TrimSpace(*in.PriorityName) == "" {
			return nil, nil, NewError(CodeInvalidArgument, "priorityName 不能为空；清空优先级暂不支持")
		}
		p, err := s.resolveNamedPriority(*in.PriorityName, schema.Priorities, "优先级")
		if err != nil {
			return nil, nil, err
		}
		if p.ID != refID(item.Priority) {
			payload.Set("priority_id", p.ID)
			changes = append(changes, FieldChange{Field: "priority", From: refName(item.Priority), To: p.Name})
		}
	}
	if in.AssigneeName != nil {
		if strings.TrimSpace(*in.AssigneeName) == "" {
			if refID(item.Assignee) != "" {
				payload.Clear("assignee_id")
				changes = append(changes, FieldChange{Field: "assignee", From: firstNonEmpty(refDisplay(item.Assignee), refName(item.Assignee)), To: nil})
			}
		} else {
			m, err := s.resolveMember(*in.AssigneeName, schema.Members)
			if err != nil {
				return nil, nil, err
			}
			if m.ID != refID(item.Assignee) {
				payload.Set("assignee_id", m.ID)
				changes = append(changes, FieldChange{Field: "assignee", From: firstNonEmpty(refDisplay(item.Assignee), refName(item.Assignee)), To: *in.AssigneeName})
			}
		}
	}
	if in.Parent != nil {
		if strings.TrimSpace(*in.Parent) == "" {
			if refID(item.Parent) != "" {
				payload.Clear("parent_id")
				changes = append(changes, FieldChange{Field: "parent", From: refID(item.Parent), To: nil})
			}
		} else {
			parentID, err := s.resolveParentID(ctx, *in.Parent, schema.Project.ID)
			if err != nil {
				return nil, nil, err
			}
			if parentID != refID(item.Parent) {
				payload.Set("parent_id", parentID)
				changes = append(changes, FieldChange{Field: "parent", From: refID(item.Parent), To: parentID})
			}
		}
	}
	if in.Properties != nil {
		// Explicit empty object must be sent; do not treat as omitempty.
		same := mapsEqual(item.Properties, in.Properties)
		if !same {
			payload.Set("properties", in.Properties)
			changes = append(changes, FieldChange{Field: "properties", From: item.Properties, To: in.Properties})
		}
	}
	return payload, changes, nil
}

func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 && len(b) == 0 {
		// Treat nil and empty map as equal for no-change detection.
		return true
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		ab, _ := json.Marshal(av)
		bb, _ := json.Marshal(bv)
		if !bytes.Equal(ab, bb) {
			return false
		}
	}
	return true
}

func (s *Service) resolveLegalTransitions(ctx context.Context, schema SchemaContext, currentStateID, toStateID string) (map[string]any, error) {
	fallback := map[string]any{
		"allowedTransitions": nil,
		"transitionAllowed":  nil,
		"note":               "未能解析状态方案，无法预检合法流转，目标以实际 PATCH 为准。",
	}
	if currentStateID == "" {
		return fallback, nil
	}
	plans, err := s.client.GetWorkItemStatePlans(ctx, schema.Project.ID)
	if err != nil {
		return fallback, nil
	}
	plan := findStatePlan(plans, schema.Project, schema.Type)
	if plan == nil {
		return fallback, nil
	}
	flows, err := s.client.GetWorkItemStateFlows(ctx, plan.ID, currentStateID)
	if err != nil {
		return fallback, nil
	}
	allowed := summarizeFlows(flows, schema.States)
	var transitionAllowed any
	if toStateID != "" {
		ok := false
		for _, entry := range allowed {
			if entry["id"] == toStateID {
				ok = true
				break
			}
		}
		transitionAllowed = ok
	}
	return map[string]any{
		"allowedTransitions": allowed,
		"transitionAllowed":  transitionAllowed,
		"note":               "已基于工作流预检合法流转。",
	}, nil
}

func assertExpectedStatus(item WorkItem, expected string) (any, error) {
	if expected == "" {
		return nil, nil
	}
	current := refName(item.State)
	if current != expected {
		return nil, NewError(CodeExpectedStateMismatch, fmt.Sprintf(
			"当前状态不符合预期：%s 当前为 %s，预期为 %s",
			firstNonEmpty(item.Identifier, item.ID),
			firstNonEmpty(current, "未知"),
			expected,
		))
	}
	return true, nil
}

func findStatePlan(plans []WorkItemStatePlan, project Project, typ WorkItemType) *WorkItemStatePlan {
	for i := range plans {
		p := &plans[i]
		typeMatch := p.WorkItemType == typ.ID || p.WorkItemType == typ.Name
		projectMatch := project.Type == "" || p.ProjectType == project.Type
		if typeMatch && projectMatch {
			return p
		}
	}
	return nil
}

func summarizeFlows(flows []WorkItemStateFlow, states []WorkItemState) []map[string]any {
	out := []map[string]any{}
	for _, flow := range flows {
		id := flow.ToStateID
		name := ""
		if flow.ToState != nil {
			id = firstNonEmpty(flow.ToState.ID, id)
			name = flow.ToState.Name
		}
		if id == "" {
			continue
		}
		if name == "" {
			for _, st := range states {
				if st.ID == id {
					name = st.Name
					break
				}
			}
		}
		out = append(out, map[string]any{"id": id, "name": name})
	}
	return out
}

func mapStates(states []WorkItemState) []map[string]any {
	out := make([]map[string]any, 0, len(states))
	for _, st := range states {
		out = append(out, map[string]any{"id": st.ID, "name": st.Name})
	}
	return out
}

func summarizeWorkItem(item WorkItem, baseURL string) WorkItemSummary {
	sources := extractImageSources(item.Description)
	url := item.HTMLURL
	if url == "" && item.Identifier != "" {
		url = strings.TrimRight(baseURL, "/") + "/pjm/work-items/" + item.Identifier
	} else if url == "" {
		url = item.URL
	}
	return WorkItemSummary{
		ID:           item.ID,
		Identifier:   item.Identifier,
		Title:        item.Title,
		State:        refName(item.State),
		Priority:     refName(item.Priority),
		Assignee:     firstNonEmpty(refDisplay(item.Assignee), refName(item.Assignee)),
		ImageCount:   len(sources),
		ImageSources: sources,
		URL:          url,
	}
}

func detailWorkItem(item WorkItem, baseURL string, includeImages bool) WorkItemDetail {
	summary := summarizeWorkItem(item, baseURL)
	if !includeImages {
		summary.ImageCount = 0
		summary.ImageSources = nil
	}
	detail := WorkItemDetail{
		WorkItemSummary: summary,
		Description:     item.Description,
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
		Properties:      item.Properties,
	}
	if item.Parent != nil {
		detail.Parent = map[string]any{
			"id":         item.Parent.ID,
			"name":       item.Parent.Name,
			"identifier": item.Parent.DisplayName,
		}
	}
	return detail
}

func extractImageSources(description string) []string {
	re := regexp.MustCompile(`(?i)<img\b[^>]*>`)
	srcRe := regexp.MustCompile(`(?i)\bsrc="([^"]+)"`)
	out := []string{}
	for _, tag := range re.FindAllString(description, -1) {
		if m := srcRe.FindStringSubmatch(tag); len(m) == 2 {
			out = append(out, m[1])
		}
	}
	return out
}

func buildUpdatedBetween(after, before string) (string, error) {
	start, err := toEpochSeconds(after)
	if err != nil {
		return "", err
	}
	end, err := toEpochSeconds(before)
	if err != nil {
		return "", err
	}
	if start == "" && end == "" {
		return "", nil
	}
	return start + "," + end, nil
}

func toEpochSeconds(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	t, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		t, err = time.Parse("2006-01-02", trimmed)
	}
	if err != nil {
		return "", NewError(CodeInvalidInput, fmt.Sprintf("无法解析时间：%s，请使用 ISO 8601 或 yyyy-MM-dd 格式。", value))
	}
	return fmt.Sprintf("%d", t.Unix()), nil
}

func joinCap(values []string) string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
		if len(out) >= 20 {
			break
		}
	}
	return strings.Join(out, ",")
}

func normalizeName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func refID(r *Ref) string {
	if r == nil {
		return ""
	}
	return r.ID
}

func refName(r *Ref) string {
	if r == nil {
		return ""
	}
	return r.Name
}

func refDisplay(r *Ref) string {
	if r == nil {
		return ""
	}
	return r.DisplayName
}
