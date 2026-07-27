package pingcode

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/0xfe10/aicli/internal/cli"
)

// VersionInfo is injected by the build.
type VersionInfo struct {
	CLI     string
	Commit  string
	Go      string
	Restish string
}

// RuntimeDependencies are assembled by main.
type RuntimeDependencies struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Stdin   io.Reader
	Version VersionInfo
	Raw     func(ctx context.Context, args []string) (RawResult, error)
}

// RawResult is returned by the transport layer and encoded once by Execute.
type RawResult struct {
	Data any
	Meta any
}

// Result is the command execution outcome.
type Result struct {
	ExitCode int
}

// Execute parses argv and runs one PingCode command.
func Execute(ctx context.Context, args []string, deps RuntimeDependencies) Result {
	if deps.Stdout == nil {
		deps.Stdout = os.Stdout
	}
	if deps.Stderr == nil {
		deps.Stderr = os.Stderr
	}
	if deps.Stdin == nil {
		deps.Stdin = os.Stdin
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(deps.Stdout, helpText())
		return Result{ExitCode: cli.ExitOK}
	}

	cfg, err := LoadConfig()
	if err != nil {
		return writeErr(deps.Stdout, err)
	}
	store := NewAuthStore(cfg.AuthTokenPath)
	client := NewClient(cfg, store)
	auth := NewAuthService(cfg, store, client)
	svc := NewService(cfg, store, client)

	cmd := args[0]
	rest := args[1:]
	switch cmd {
	case "version":
		return writeOK(deps.Stdout, map[string]any{
			"cli":     firstNonEmpty(deps.Version.CLI, "0.1.0"),
			"commit":  firstNonEmpty(deps.Version.Commit, "unknown"),
			"go":      firstNonEmpty(deps.Version.Go, runtime.Version()),
			"restish": firstNonEmpty(deps.Version.Restish, "2.3.0"),
		}, map[string]any{"command": "version"})
	case "config":
		return runConfig(rest, cfg, deps)
	case "auth":
		return runAuth(ctx, rest, auth, deps)
	case "project":
		return runProject(ctx, rest, svc, deps)
	case "work-item":
		return runWorkItem(ctx, rest, svc, deps)
	case "raw":
		if hasHelp(rest) {
			fmt.Fprint(deps.Stdout, rawHelpText())
			return Result{ExitCode: cli.ExitOK}
		}
		if deps.Raw == nil {
			return writeErr(deps.Stdout, NewError(CodeInternalError, "raw transport 未初始化"))
		}
		result, err := deps.Raw(ctx, rest)
		if err != nil {
			return writeRawFailure(deps.Stdout, result, err)
		}
		return writeOK(deps.Stdout, result.Data, result.Meta)
	default:
		_ = cli.UnknownCommand(deps.Stdout, args)
		return Result{ExitCode: cli.ExitUsage}
	}
}

func runConfig(args []string, cfg Config, deps RuntimeDependencies) Result {
	if hasHelp(args) || len(args) == 0 {
		fmt.Fprint(deps.Stdout, configHelpText())
		return Result{ExitCode: cli.ExitOK}
	}
	if args[0] != "check" {
		return writeErr(deps.Stdout, NewError(CodeUnknownCommand, "Unknown command: config "+strings.Join(args, " ")))
	}
	data := CheckConfig(cfg)
	meta := map[string]any{"command": "config check"}
	if ok, _ := data["ok"].(bool); !ok {
		_ = cli.WriteJSON(deps.Stdout, cli.Response{OK: false, Error: &cli.ErrorBody{
			Code:    CodeConfigMissing,
			Message: "配置检查未通过",
		}, Data: data, Meta: meta})
		return Result{ExitCode: cli.ExitConfig}
	}
	return writeOK(deps.Stdout, data, meta)
}

func runAuth(ctx context.Context, args []string, auth *AuthService, deps RuntimeDependencies) Result {
	if len(args) == 0 || hasHelp(args) {
		fmt.Fprint(deps.Stdout, authHelpText())
		return Result{ExitCode: cli.ExitOK}
	}
	switch args[0] {
	case "status":
		status, err := auth.Status(ctx)
		if err != nil {
			return writeErr(deps.Stdout, err)
		}
		return writeOK(deps.Stdout, status, map[string]any{"command": "auth status"})
	case "login":
		data, err := auth.BuildAuthorizeURL()
		if err != nil {
			return writeErr(deps.Stdout, err)
		}
		return writeOK(deps.Stdout, data, map[string]any{"command": "auth login"})
	case "complete":
		fs := flag.NewFlagSet("auth complete", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		callbackStdin := fs.Bool("callback-url-stdin", false, "read callback URL from stdin")
		if err := fs.Parse(args[1:]); err != nil {
			return writeErr(deps.Stdout, NewError(CodeInvalidInput, err.Error()))
		}
		if !*callbackStdin {
			return writeErr(deps.Stdout, NewError(CodeInvalidInput, "必须使用 --callback-url-stdin，授权码不得进入 argv"))
		}
		raw, err := io.ReadAll(io.LimitReader(deps.Stdin, 1<<20))
		if err != nil {
			return writeErr(deps.Stdout, WrapError(CodeInvalidInput, "无法读取 stdin", err))
		}
		data, err := auth.CompleteFromCallbackURL(ctx, string(raw))
		if err != nil {
			return writeErr(deps.Stdout, err)
		}
		return writeOK(deps.Stdout, data, map[string]any{"command": "auth complete"})
	case "logout":
		data, err := auth.Logout()
		if err != nil {
			return writeErr(deps.Stdout, err)
		}
		return writeOK(deps.Stdout, data, map[string]any{"command": "auth logout"})
	default:
		return writeErr(deps.Stdout, NewError(CodeUnknownCommand, "Unknown command: auth "+args[0]))
	}
}

func runProject(ctx context.Context, args []string, svc *Service, deps RuntimeDependencies) Result {
	if len(args) == 0 || hasHelp(args) {
		fmt.Fprint(deps.Stdout, projectHelpText())
		return Result{ExitCode: cli.ExitOK}
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("project list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		identifier := fs.String("identifier", "", "project identifier")
		pageIndex := fs.Int("page-index", 0, "page index")
		pageSize := fs.Int("page-size", 30, "page size")
		if err := fs.Parse(args[1:]); err != nil {
			return writeErr(deps.Stdout, NewError(CodeInvalidInput, err.Error()))
		}
		page, err := svc.ListProjects(ctx, *identifier, *pageIndex, *pageSize)
		if err != nil {
			return writeErr(deps.Stdout, err)
		}
		truncated := page.Total > (page.PageIndex+1)*page.PageSize
		return writeOK(deps.Stdout, page, map[string]any{
			"command":   "project list",
			"truncated": truncated,
			"pageIndex": page.PageIndex,
			"pageSize":  page.PageSize,
			"total":     page.Total,
		})
	case "schema":
		fs := flag.NewFlagSet("project schema", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		kind := fs.String("kind", "", "bug|requirement")
		identifier := fs.String("identifier", "", "project identifier")
		projectID := fs.String("project-id", "", "project id")
		typeID := fs.String("type-id", "", "type id override")
		if err := fs.Parse(args[1:]); err != nil {
			return writeErr(deps.Stdout, NewError(CodeInvalidInput, err.Error()))
		}
		var kindPtr *WorkItemKind
		if *kind != "" {
			k := WorkItemKind(*kind)
			kindPtr = &k
		}
		data, err := svc.GetSchema(ctx, kindPtr, *identifier, *projectID, *typeID)
		if err != nil {
			return writeErr(deps.Stdout, err)
		}
		return writeOK(deps.Stdout, data, map[string]any{"command": "project schema"})
	default:
		return writeErr(deps.Stdout, NewError(CodeUnknownCommand, "Unknown command: project "+args[0]))
	}
}

func runWorkItem(ctx context.Context, args []string, svc *Service, deps RuntimeDependencies) Result {
	if len(args) == 0 || hasHelp(args) {
		fmt.Fprint(deps.Stdout, workItemHelpText(args))
		return Result{ExitCode: cli.ExitOK}
	}
	switch args[0] {
	case "get":
		fs := flag.NewFlagSet("work-item get", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("id", "", "work item id")
		identifier := fs.String("identifier", "", "work item identifier")
		kind := fs.String("kind", "bug", "bug|requirement")
		includeComments := fs.Bool("include-comments", false, "include comments")
		projectIdentifier := fs.String("project-identifier", "", "project identifier")
		projectID := fs.String("project-id", "", "project id")
		if err := fs.Parse(args[1:]); err != nil {
			return writeErr(deps.Stdout, NewError(CodeInvalidInput, err.Error()))
		}
		data, err := svc.GetWorkItemDetail(ctx, WorkItemKind(*kind), *id, *identifier, *includeComments, *projectIdentifier, *projectID)
		if err != nil {
			return writeErr(deps.Stdout, err)
		}
		return writeOK(deps.Stdout, data, map[string]any{"command": "work-item get"})
	case "search":
		fs := flag.NewFlagSet("work-item search", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		kinds := fs.String("kinds", "bug,requirement", "comma-separated kinds")
		keywords := fs.String("keywords", "", "keywords")
		states := fs.String("states", "", "comma-separated state names")
		priorities := fs.String("priorities", "", "comma-separated priority names")
		assignees := fs.String("assignees", "", "comma-separated assignee names")
		updatedAfter := fs.String("updated-after", "", "updated after")
		updatedBefore := fs.String("updated-before", "", "updated before")
		pageIndex := fs.Int("page-index", 0, "page index")
		pageSize := fs.Int("page-size", 30, "page size")
		projectIdentifier := fs.String("project-identifier", "", "project identifier")
		projectID := fs.String("project-id", "", "project id")
		if err := fs.Parse(args[1:]); err != nil {
			return writeErr(deps.Stdout, NewError(CodeInvalidInput, err.Error()))
		}
		data, err := svc.SearchWorkItems(ctx, SearchOptions{
			Kinds:             parseKinds(*kinds),
			Keywords:          *keywords,
			StateNames:        splitCSV(*states),
			PriorityNames:     splitCSV(*priorities),
			AssigneeNames:     splitCSV(*assignees),
			UpdatedAfter:      *updatedAfter,
			UpdatedBefore:     *updatedBefore,
			PageIndex:         *pageIndex,
			PageSize:          *pageSize,
			ProjectIdentifier: *projectIdentifier,
			ProjectID:         *projectID,
		})
		if err != nil {
			return writeErr(deps.Stdout, err)
		}
		return writeOK(deps.Stdout, data, map[string]any{"command": "work-item search"})
	case "mine":
		fs := flag.NewFlagSet("work-item mine", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		assignee := fs.String("assignee", "", "assignee override")
		kinds := fs.String("kinds", "bug,requirement", "kinds")
		states := fs.String("states", "", "state names")
		updatedAfter := fs.String("updated-after", "", "updated after")
		updatedBefore := fs.String("updated-before", "", "updated before")
		pageSize := fs.Int("page-size", 30, "page size")
		projectIdentifier := fs.String("project-identifier", "", "project identifier")
		projectID := fs.String("project-id", "", "project id")
		if err := fs.Parse(args[1:]); err != nil {
			return writeErr(deps.Stdout, NewError(CodeInvalidInput, err.Error()))
		}
		data, err := svc.GetMyWork(ctx, MyWorkOptions{
			AssigneeName:      *assignee,
			Kinds:             parseKinds(*kinds),
			StateNames:        splitCSV(*states),
			UpdatedAfter:      *updatedAfter,
			UpdatedBefore:     *updatedBefore,
			PageSize:          *pageSize,
			ProjectIdentifier: *projectIdentifier,
			ProjectID:         *projectID,
		})
		if err != nil {
			return writeErr(deps.Stdout, err)
		}
		return writeOK(deps.Stdout, data, map[string]any{"command": "work-item mine"})
	case "create", "update", "transition", "comment":
		return runWrite(ctx, args[0], args[1:], svc, deps)
	default:
		return writeErr(deps.Stdout, NewError(CodeUnknownCommand, "Unknown command: work-item "+args[0]))
	}
}

func runWrite(ctx context.Context, action string, args []string, svc *Service, deps RuntimeDependencies) Result {
	if hasHelp(args) {
		fmt.Fprint(deps.Stdout, workItemHelpText([]string{action}))
		return Result{ExitCode: cli.ExitOK}
	}
	fs := flag.NewFlagSet("work-item "+action, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	inputFlag := fs.String("input", "", "input source; use - for stdin JSON")
	apply := fs.Bool("apply", false, "execute write")
	if err := fs.Parse(args); err != nil {
		return writeErr(deps.Stdout, NewError(CodeInvalidInput, err.Error()))
	}
	if *inputFlag != "-" {
		return writeErr(deps.Stdout, NewError(CodeInvalidInput, "写命令必须使用 --input - 从 stdin 读取 JSON"))
	}
	raw, err := io.ReadAll(io.LimitReader(deps.Stdin, 4<<20))
	if err != nil {
		return writeErr(deps.Stdout, WrapError(CodeInvalidArgument, "无法读取 stdin", err))
	}
	if err := rejectWriteControlFields(raw); err != nil {
		return writeErr(deps.Stdout, err)
	}
	var data any
	switch action {
	case "create":
		var in CreateInput
		if err := DecodeStrictJSON(raw, &in); err != nil {
			return writeErr(deps.Stdout, err)
		}
		data, err = svc.CreateWorkItem(ctx, in, *apply)
	case "update":
		var in UpdateInput
		if err := DecodeStrictJSON(raw, &in); err != nil {
			return writeErr(deps.Stdout, err)
		}
		data, err = svc.UpdateWorkItemFields(ctx, in, *apply)
	case "transition":
		var in TransitionInput
		if err := DecodeStrictJSON(raw, &in); err != nil {
			return writeErr(deps.Stdout, err)
		}
		data, err = svc.TransitionWorkItem(ctx, in, *apply)
	case "comment":
		var in CommentInput
		if err := DecodeStrictJSON(raw, &in); err != nil {
			return writeErr(deps.Stdout, err)
		}
		data, err = svc.AddComment(ctx, in, *apply)
	}
	if err != nil {
		return writeErr(deps.Stdout, err)
	}
	return writeOK(deps.Stdout, data, map[string]any{
		"command": "work-item " + action,
		"applied": *apply,
	})
}

func rejectWriteControlFields(raw []byte) error {
	// Loose probe only for clearer apply/dryRun messages; typed decode enforces unknown fields.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(raw), &probe); err != nil {
		return nil
	}
	if _, ok := probe["apply"]; ok {
		return NewError(CodeInvalidArgument, "写入 JSON 不得包含 apply；请使用顶层 --apply")
	}
	if _, ok := probe["dryRun"]; ok {
		return NewError(CodeInvalidArgument, "写入 JSON 不得包含 dryRun；请使用顶层 --apply")
	}
	return nil
}

func writeOK(w io.Writer, data any, meta any) Result {
	_ = cli.WriteOK(w, data, meta)
	return Result{ExitCode: cli.ExitOK}
}

func writeErr(w io.Writer, err error) Result {
	pe := Classify(err)
	msg := Redact(pe.Message)
	_ = cli.WriteError(w, pe.Code, msg)
	return Result{ExitCode: cli.ExitCodeFor(pe.Code)}
}

func writeRawFailure(w io.Writer, result RawResult, err error) Result {
	pe := Classify(err)
	msg := Redact(pe.Message)
	_ = cli.WriteJSON(w, cli.Response{
		OK:   false,
		Data: result.Data,
		Error: &cli.ErrorBody{
			Code:    pe.Code,
			Message: msg,
		},
		Meta: result.Meta,
	})
	return Result{ExitCode: cli.ExitCodeFor(pe.Code)}
}

func hasHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseKinds(raw string) []WorkItemKind {
	parts := splitCSV(raw)
	out := make([]WorkItemKind, 0, len(parts))
	for _, p := range parts {
		out = append(out, WorkItemKind(p))
	}
	return out
}

func helpText() string {
	return `pingcode

PingCode CLI for AI-oriented project workflows.

Usage:
  pingcode version
  pingcode config check
  pingcode auth status|login|complete|logout
  pingcode project list|schema
  pingcode work-item get|search|mine|create|update|transition|comment
  pingcode raw <METHOD> <path> [--body JSON]

Writes default to dry-run. Pass --apply to execute.
Use "<command> --help" for command-specific usage and stdin JSON fields.
`
}

func configHelpText() string {
	return `pingcode config check

Validate local PingCode configuration without revealing credentials.
`
}

func authHelpText() string {
	return `pingcode auth

Usage:
  pingcode auth status
  pingcode auth login
  pingcode auth complete --callback-url-stdin
  pingcode auth logout

auth complete reads the OAuth callback URL from stdin.
`
}

func projectHelpText() string {
	return `pingcode project

Usage:
  pingcode project list [--identifier ID] [--page-index N] [--page-size N]
  pingcode project schema [--kind bug|requirement] [--identifier ID] [--project-id ID] [--type-id ID]
`
}

func workItemHelpText(args []string) string {
	action := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action = args[0]
	}
	switch action {
	case "get":
		return `pingcode work-item get

Usage:
  pingcode work-item get (--id ID | --identifier DEMO-1) [--kind bug|requirement]
                         [--include-comments] [--project-identifier ID] [--project-id ID]
`
	case "search":
		return `pingcode work-item search

Usage:
  pingcode work-item search [--kinds bug,requirement] [--keywords TEXT]
                            [--states ...] [--priorities ...] [--assignees ...]
                            [--updated-after ...] [--updated-before ...]
                            [--page-index N] [--page-size N]
                            [--project-identifier ID] [--project-id ID]
`
	case "mine":
		return `pingcode work-item mine

Usage:
  pingcode work-item mine [--assignee NAME] [--kinds bug,requirement] [--states ...]
                          [--updated-after ...] [--updated-before ...] [--page-size N]
                          [--project-identifier ID] [--project-id ID]

Requires --assignee or PINGCODE_DEFAULT_ASSIGNEE_NAME.
`
	case "create":
		return `pingcode work-item create

Usage:
  pingcode work-item create --input - [--apply]

stdin JSON fields:
  kind, title (required), description, priorityName, assigneeName,
  statusName, parent, properties, projectIdentifier, projectId

Default is dry-run. Pass --apply to create.
`
	case "update":
		return `pingcode work-item update

Usage:
  pingcode work-item update --input - [--apply]

stdin JSON fields:
  kind, workItemId|identifier, expectedCurrentState (recommended),
  title, description, priorityName, assigneeName, parent, properties,
  projectIdentifier, projectId

description="" clears to PingCode empty rich text (<p></p>).
Default is dry-run. Pass --apply to update.
`
	case "transition":
		return `pingcode work-item transition

Usage:
  pingcode work-item transition --input - [--apply]

stdin JSON fields:
  kind, workItemId|identifier, statusName|stateId,
  expectedCurrentState (recommended), comment,
  projectIdentifier, projectId

Default is dry-run. Pass --apply to transition.
`
	case "comment":
		return `pingcode work-item comment

Usage:
  pingcode work-item comment --input - [--apply]

stdin JSON fields:
  kind, workItemId|identifier, content (required),
  projectIdentifier, projectId

Default is dry-run. Pass --apply to comment.
`
	default:
		return `pingcode work-item

Usage:
  pingcode work-item get|search|mine|create|update|transition|comment [--help]

Writes (create/update/transition/comment) read one JSON document from stdin via --input -
and default to dry-run unless --apply is set.
`
	}
}

func rawHelpText() string {
	return `pingcode raw

Controlled HTTP escape hatch for PingCode API debugging.
Restish is linked into the binary for version/compliance only; requests use net/http.

Usage:
  pingcode raw <GET|POST|PATCH|PUT|DELETE|HEAD> <path> [--body JSON]

Path must be a relative API path such as /v1/project/projects.
Stdout is always exactly one JSON document.
`
}

// ParseIntFlag is a tiny helper for tests.
func ParseIntFlag(raw string, fallback int) int {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}
