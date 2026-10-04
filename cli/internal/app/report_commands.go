package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
	"agent-nexus-cli/internal/visualreport"
)

var reportSubcommandSpec = subcommandSpec{
	command: "report",
	valid:   []string{"schema", "validate", "publish"},
	examples: []string{
		"anx report schema",
		"anx report validate ./dashboard.json",
		"anx report publish ./dashboard.json --topic topic:launch",
	},
}

func isReportLocalCommand(args []string) bool {
	if len(args) == 0 || args[0] != "report" {
		return false
	}
	if len(args) == 1 {
		return true
	}
	return reportSubcommandSpec.normalize(args[1]) != "render"
}

func init() {
	localHelperTopics = append(localHelperTopics,
		localHelperTopic{
			Path: "report schema", Summary: "Print the visual report types, limits, and minimal example.",
			JSONShape:   "`kind`, `schema_version`, `panel_types`, `limits`, `example`",
			Composition: "Pure local helper; no credentials or network required.",
			Examples:    []string{"anx report schema"},
		},
		localHelperTopic{
			Path: "report validate", Summary: "Validate a visual report file or stdin against the renderer's schema.",
			JSONShape:   "`recognized`, `valid`, `errors`, `panel_count`",
			Composition: "Pure local helper using the same report contract as the web renderer.",
			Examples:    []string{"anx report validate ./dashboard.json", "cat dashboard.json | anx report validate -"},
			Flags:       []localHelperFlag{{Name: "<file|->", Description: "Report JSON path, or - to read stdin."}},
		},
		localHelperTopic{
			Path: "report publish", Summary: "Validate, publish, read back, and revalidate a visual report document.",
			JSONShape:   "`doc_ref`, optional `web_url`, `title`, `action`, `panel_count`, `validated`",
			Composition: "Creates in the topic thread or revises a matching visual report. An explicit --doc resolves exactly; replacing another document requires --replace.",
			Examples:    []string{"anx report publish ./dashboard.json --topic topic:launch", "anx report publish ./dashboard.json --topic topic:launch --title \"Fleet Dashboard\" --doc doc:fleet-dashboard --replace"},
			Flags: []localHelperFlag{
				{Name: "<file>", Description: "Visual report JSON path."},
				{Name: "--topic <ref>", Description: "Existing topic to anchor the report."},
				{Name: "--title <text>", Description: "Document title; defaults to the report title."},
				{Name: "--doc <ref>", Description: "Existing document in the topic to revise."},
				{Name: "--replace", Description: "Allow replacing an explicitly selected non-report document."},
			},
		},
	)
}

func reportSchema() map[string]any {
	return map[string]any{
		"kind":           visualreport.Kind,
		"schema_version": visualreport.Version,
		"panel_types":    visualreport.PanelTypes(),
		"limits": map[string]any{
			"bytes": 131072, "projects": 16, "sources": 64, "panels": 32, "title_chars": 200,
			"text_chars": 12000, "cell_chars": 2000, "url_chars": 2048, "columns": 12,
			"rows": 200, "milestones": 100, "nodes": 40, "edges": 80, "points": 200,
			"chart_series": 12, "chart_total_points": 1200, "chart_nodes": 120, "chart_links": 240,
			"chart_depth": 5, "chart_categories": 12, "chart_mark_lines": 6,
			"chart_label_chars": 200, "chart_caption_chars": 2000, "chart_magnitude": 1000000000000,
			"chart_min_nonzero_magnitude": 1e-100, "layout_nodes": 100, "layout_depth": 6,
			"layout_children": 32, "layout_tabs": 8, "errors": 20,
		},
		"example": map[string]any{
			"kind": visualreport.Kind, "schema_version": visualreport.Version, "title": "Project evidence report",
			"summary": "A report with an explicit evidence boundary.", "generated_at": "2026-10-03T06:38:37Z",
			"projects": []any{map[string]any{"id": "project-a", "title": "Project A", "summary": "No operational evidence has been collected.", "outcome": "Qualification unknown"}},
			"sources":  []any{},
			"panels": []any{map[string]any{
				"id": "qualification", "project_id": "project-a", "type": "explanation", "title": "Evidence still needed",
				"author": "unknown", "provenance": "reported", "observed_at": nil, "freshness": "unavailable", "source_ids": []any{},
				"data": map[string]any{"text": "No observation is available. This does not establish health or completion."},
			}},
		},
	}
}

func (a *App) runReportCommand(ctx context.Context, args []string, cfg config.Resolved) (string, *commandResult, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		text, _ := helpTopicText("report")
		return "report", &commandResult{Text: text, Data: map[string]any{"help_text": text}}, nil
	}
	sub := reportSubcommandSpec.normalize(args[0])
	switch sub {
	case "schema":
		if len(args) != 1 {
			return "report schema", nil, errnorm.Usage("invalid_args", "unexpected arguments for `anx report schema`")
		}
		data := reportSchema()
		text := "Visual report schema v1. Use `anx report publish` to give humans a dashboard."
		return "report schema", &commandResult{Text: text, Data: data}, nil
	case "validate":
		result, err := a.runReportValidate(args[1:])
		return "report validate", result, err
	case "publish":
		result, err := a.runReportPublish(ctx, args[1:], cfg)
		return "report publish", result, err
	default:
		return "report", nil, reportSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) reportInput(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "-" {
		if a.Stdin == nil {
			return nil, errnorm.Usage("invalid_request", "stdin is not available")
		}
		content, err := io.ReadAll(io.LimitReader(a.Stdin, visualreport.MaxBytes+1))
		if err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "input_read_failed", "failed to read visual report from stdin", err)
		}
		return content, nil
	}
	if path == "" {
		return nil, errnorm.Usage("invalid_args", "report file is required")
	}
	readFile := a.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	content, err := readFile(path)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "input_read_failed", "failed to read visual report file", err)
	}
	if len(content) > visualreport.MaxBytes+1 {
		content = content[:visualreport.MaxBytes+1]
	}
	return content, nil
}

func reportValidationError(result visualreport.Result) error {
	if !result.Recognized {
		return errnorm.Usage("invalid_visual_report", "input is not a recognized anx.visual-report JSON document")
	}
	if result.Valid {
		return nil
	}
	message := "visual report validation failed"
	if len(result.Errors) > 0 {
		message += ": " + strings.Join(result.Errors, "; ")
	}
	return errnorm.WithDetails(errnorm.Usage("invalid_visual_report", message), map[string]any{"errors": result.Errors})
}

func (a *App) runReportValidate(args []string) (*commandResult, error) {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") && args[0] != "-" {
		return nil, errnorm.Usage("invalid_args", "usage: anx report validate <file|->")
	}
	content, err := a.reportInput(args[0])
	if err != nil {
		return nil, err
	}
	result := visualreport.Validate(content)
	if err := reportValidationError(result); err != nil {
		return nil, err
	}
	report, _ := result.Report.(map[string]any)
	return &commandResult{Text: fmt.Sprintf("Visual report is valid (%d panels).", len(asSlice(report["panels"]))), Data: map[string]any{"recognized": true, "valid": true, "errors": []string{}, "panel_count": len(asSlice(report["panels"]))}}, nil
}

func reportContentWarning(content any) []output.Warning {
	if content == nil {
		return nil
	}
	var bytes []byte
	switch value := content.(type) {
	case string:
		bytes = []byte(value)
	default:
		var err error
		bytes, err = json.Marshal(value)
		if err != nil {
			return nil
		}
	}
	result := visualreport.Validate(bytes)
	if !result.Recognized || result.Valid {
		return nil
	}
	first := result.Errors
	if len(first) > 3 {
		first = first[:3]
	}
	message := "Document content looks like an anx.visual-report but failed validation"
	if len(first) > 0 {
		message += ": " + strings.Join(first, "; ")
	}
	return []output.Warning{{Code: "invalid_visual_report", Message: message, Details: map[string]any{"errors": result.Errors}}}
}

func reportBodyContent(body map[string]any) any {
	if body == nil {
		return nil
	}
	if content, ok := body["content"]; ok {
		return content
	}
	return extractNestedMap(body, "revision")["content"]
}

type reportPublishArgs struct {
	file, topic, title, document string
	replace                      bool
}

func parseReportPublishArgs(args []string) (reportPublishArgs, error) {
	file, rest := popLeadingPositional(args)
	fs := newSilentFlagSet("report publish")
	var topic, title, doc trackedString
	var replace trackedBool
	fs.Var(&topic, "topic", "Existing topic ref or handle")
	fs.Var(&title, "title", "Document title")
	fs.Var(&doc, "doc", "Existing document ref or handle")
	fs.Var(&replace, "replace", "Allow replacing an explicitly selected non-report document")
	if err := fs.Parse(rest); err != nil {
		return reportPublishArgs{}, errnorm.Usage("invalid_flags", err.Error())
	}
	if file == "" {
		pos := fs.Args()
		if len(pos) > 0 {
			file = pos[0]
			pos = pos[1:]
		}
		if len(pos) > 0 {
			return reportPublishArgs{}, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx report publish`")
		}
	}
	if file == "" || file == "-" {
		return reportPublishArgs{}, errnorm.Usage("invalid_args", "usage: anx report publish <file> --topic <ref>")
	}
	if len(fs.Args()) > 0 {
		return reportPublishArgs{}, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx report publish`")
	}
	if strings.TrimSpace(topic.value) == "" {
		return reportPublishArgs{}, errnorm.Usage("invalid_request", "`--topic` is required for `anx report publish`")
	}
	if replace.set && replace.value && strings.TrimSpace(doc.value) == "" {
		return reportPublishArgs{}, errnorm.Usage("invalid_request", "`--replace` requires an explicit `--doc` ref")
	}
	return reportPublishArgs{file: file, topic: normalizeTypedRefFlag(topic.value, "topic"), title: strings.TrimSpace(title.value), document: strings.TrimSpace(doc.value), replace: replace.set && replace.value}, nil
}

func (a *App) runReportPublish(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	input, err := parseReportPublishArgs(args)
	if err != nil {
		return nil, err
	}
	content, err := a.reportInput(input.file)
	if err != nil {
		return nil, err
	}
	parsed := visualreport.Validate(content)
	if err := reportValidationError(parsed); err != nil {
		return nil, err
	}
	report, _ := parsed.Report.(map[string]any)
	docTitle := input.title
	if docTitle == "" {
		docTitle, _ = report["title"].(string)
	}
	if strings.TrimSpace(docTitle) == "" {
		return nil, errnorm.Usage("invalid_request", "visual report title is required")
	}

	docs, topicID, threadID, err := a.listDocumentsForReportTopic(ctx, cfg, input.topic)
	if err != nil {
		return nil, err
	}
	var target map[string]any
	var targetRead *commandResult
	if input.document != "" {
		matches := findExplicitReportDocuments(docs, input.document)
		if len(matches) == 0 {
			return nil, errnorm.WithDetails(errnorm.Usage("document_not_in_topic", "the requested document was not found in the selected topic"), map[string]any{"topic": input.topic, "document": input.document})
		}
		if len(matches) > 1 {
			return nil, errnorm.WithDetails(errnorm.Usage("ambiguous_document", "the requested document ref matches more than one document in the selected topic"), map[string]any{"document": input.document})
		}
		target, targetRead, err = a.readReportTarget(ctx, cfg, matches[0])
		if err != nil {
			return nil, err
		}
		if !isVisualReportDocument(targetRead) && !input.replace {
			return nil, errnorm.WithDetails(errnorm.Usage("replace_required", "the selected document is not a visual report; pass `--replace` to replace it explicitly"), map[string]any{"document": input.document})
		}
	} else {
		candidates := findAutomaticReportDocuments(docs, docTitle)
		var reportTargets []map[string]any
		for _, candidate := range candidates {
			doc, read, readErr := a.readReportTarget(ctx, cfg, candidate)
			if readErr != nil {
				return nil, readErr
			}
			if isVisualReportDocument(read) {
				target, targetRead = doc, read
				reportTargets = append(reportTargets, doc)
			}
		}
		if len(reportTargets) > 1 {
			return nil, errnorm.WithDetails(errnorm.Usage("ambiguous_report_document", "more than one visual-report document matches this title in the selected topic; pass an exact `--doc` ref"), map[string]any{"title": docTitle})
		}
	}
	if input.title == "" && input.document != "" && target != nil {
		docTitle = strings.TrimSpace(anyString(target["title"]))
		if docTitle == "" {
			docTitle, _ = report["title"].(string)
		}
	}

	contentText := string(content)
	actionName := "created"
	var targetID string
	var revisionID string
	if target == nil {
		body := docsCreateBodyFromFlags(docTitle, "", input.topic, "", nil)
		if doc, ok := body["document"].(map[string]any); ok {
			doc["subject_ref"] = input.topic
			doc["refs"] = uniqueStrings([]string{input.topic})
			doc["thread_id"] = threadID
		}
		body["content_type"] = "text"
		body["content"] = contentText
		body["request_key"] = reportCreateRequestKey(topicID, docTitle)
		if err := finalizeOptionalMutationBodyActorID(body, cfg); err != nil {
			return nil, err
		}
		if err := validateDocsCreateBody(body, "report publish"); err != nil {
			return nil, err
		}
		created, callErr := a.invokeTypedJSON(ctx, cfg, "report publish", "docs.create", nil, nil, body)
		if callErr != nil {
			return nil, callErr
		}
		response := commandResultBody(created)
		doc := extractNestedMap(response, "document")
		targetID = reportDocumentID(doc)
		target = doc
		revisionID = reportResponseRevisionID(response)
		if targetID == "" {
			return nil, errnorm.WithDetails(errnorm.Wrap(errnorm.KindLocal, "publish_readback_failed", "document creation returned no document reference", nil), map[string]any{"response": response})
		}
		if revisionID == "" {
			return nil, errnorm.WithDetails(errnorm.Wrap(errnorm.KindLocal, "publish_readback_failed", "document creation returned no revision id", nil), map[string]any{"response": response})
		}
	} else {
		actionName = "revised"
		targetID = reportDocumentID(target)
		if targetID == "" {
			return nil, errnorm.Usage("invalid_request", "could not resolve the published document ref")
		}
		body := commandResultBody(targetRead)
		base := docsHeadRevisionID(asMap(targetRead.Data))
		if base == "" {
			base = anyString(extractNestedMap(body, "revision")["revision_id"])
		}
		if base == "" {
			return nil, errnorm.Usage("invalid_request", "existing report document has no head revision")
		}
		revisionBody := map[string]any{"if_base_revision": base, "content_type": "text", "content": contentText}
		if strings.TrimSpace(input.title) != "" {
			revisionBody["title"] = input.title
		}
		preparedAny, prepErr := ensureDocsRevisionActorIdentity(revisionBody, cfg)
		if prepErr != nil {
			return nil, prepErr
		}
		prepared, prepErr := mapBody(preparedAny, "report publish")
		if prepErr != nil {
			return nil, prepErr
		}
		if prepErr = validateDocsRevisionBody(prepared, "report publish"); prepErr != nil {
			return nil, prepErr
		}
		createdRevision, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "report publish", "docs.revisions.create", "document_id", targetID, documentIDLookupSpec, nil, prepared)
		if callErr != nil {
			return nil, callErr
		}
		revisionID = reportResponseRevisionID(commandResultBody(createdRevision))
		if revisionID == "" {
			return nil, errnorm.WithDetails(errnorm.Wrap(errnorm.KindLocal, "publish_readback_failed", "revision creation returned no revision id", nil), map[string]any{"response": commandResultBody(createdRevision)})
		}
	}
	if targetID == "" {
		return nil, errnorm.Usage("invalid_request", "could not resolve the published document ref")
	}
	readback, err := a.getReportRevision(ctx, cfg, targetID, revisionID)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "publish_readback_failed", "could not read back the written report revision", err)
	}
	readRevision := extractNestedMap(commandResultBody(readback), "revision")
	if anyString(readRevision["revision_id"]) != revisionID {
		return nil, errnorm.WithDetails(errnorm.Wrap(errnorm.KindLocal, "publish_readback_failed", "readback returned a different revision than the one just written", nil), map[string]any{"expected_revision_id": revisionID, "actual_revision_id": anyString(readRevision["revision_id"])})
	}
	if anyString(readRevision["content_type"]) != "text" {
		return nil, errnorm.WithDetails(errnorm.Wrap(errnorm.KindLocal, "publish_readback_failed", "published revision is not stored as text", nil), map[string]any{"expected_content_type": "text", "actual_content_type": anyString(readRevision["content_type"])})
	}
	readContent, ok := readRevision["content"].(string)
	if !ok || !bytes.Equal([]byte(readContent), content) {
		return nil, errnorm.WithDetails(errnorm.Wrap(errnorm.KindLocal, "publish_readback_failed", "published revision content differs from the submitted bytes", nil), map[string]any{"expected_bytes": len(content), "actual_bytes": len(readContent)})
	}
	verified := visualreport.Validate([]byte(readContent))
	if err := reportValidationError(verified); err != nil {
		return nil, errnorm.WithDetails(errnorm.Wrap(errnorm.KindLocal, "publish_readback_invalid", "published document readback failed visual report validation", err), map[string]any{"doc_ref": targetID, "errors": verified.Errors})
	}
	responseDoc := target
	ref := firstNonEmpty(anyString(responseDoc["ref"]), anyString(responseDoc["handle"]), anyString(responseDoc["id"]), targetID)
	result := &commandResult{Data: map[string]any{"doc_ref": ref, "title": docTitle, "action": actionName, "panel_count": len(asSlice(report["panels"])), "validated": true, "content_type": "text"}}
	if webURL, err := resourceURL(cfg, resourceLocator{Kind: "document", ID: reportDocumentRouteID(responseDoc, ref)}); err == nil && webURL != "" {
		result.Data.(map[string]any)["web_url"] = webURL
	}
	result.Text = fmt.Sprintf("Visual report %s and validated. Document: %s.", actionName, ref)
	if webURL := anyString(result.Data.(map[string]any)["web_url"]); webURL != "" {
		result.Text += " URL: " + webURL
	}
	return result, nil
}

func (a *App) listDocumentsForReportTopic(ctx context.Context, cfg config.Resolved, topic string) ([]any, string, string, error) {
	topicID := strings.TrimPrefix(strings.TrimSpace(topic), "topic:")
	if topicID == "" {
		return nil, "", "", errnorm.Usage("invalid_request", "`--topic` must name an existing topic")
	}
	topicResult, err := a.invokeTypedJSONWithIDResolution(ctx, cfg, "report publish", "topics.get", "topic_id", topicID, topicIDLookupSpec, nil, nil)
	if err != nil {
		return nil, "", "", err
	}
	topicData := extractNestedMap(commandResultBody(topicResult), "topic")
	topicID = firstNonEmpty(anyString(topicData["id"]), topicID)
	threadID := strings.TrimSpace(anyString(topicData["thread_id"]))
	if threadID == "" {
		return nil, "", "", errnorm.Usage("invalid_request", "could not resolve the topic backing thread")
	}
	var docs []any
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		query := []queryParam{}
		addSingleQuery(&query, "thread_id", threadID)
		addSingleQuery(&query, "limit", "1000")
		addSingleQuery(&query, "cursor", cursor)
		result, callErr := a.invokeTypedJSON(ctx, cfg, "report publish", "docs.list", nil, query, nil)
		if callErr != nil {
			return nil, "", "", callErr
		}
		body := commandResultBody(result)
		docs = append(docs, asSlice(body["documents"])...)
		next := firstNonEmpty(anyString(body["next_cursor"]), anyString(body["cursor_next"]))
		if next == "" {
			break
		}
		if seen[next] {
			return nil, "", "", errnorm.Usage("invalid_response", "document list returned a repeated cursor")
		}
		seen[next] = true
		cursor = next
		if page == 99 {
			return nil, "", "", errnorm.Usage("invalid_response", "document list exceeded the pagination safety limit")
		}
	}
	return docs, topicID, threadID, nil
}

func findExplicitReportDocuments(docs []any, key string) []map[string]any {
	wanted := map[string]struct{}{strings.TrimSpace(key): {}}
	if !strings.Contains(key, ":") {
		wanted["doc:"+strings.TrimSpace(key)] = struct{}{}
	} else if strings.HasPrefix(key, "doc:") {
		wanted["document:"+strings.TrimPrefix(key, "doc:")] = struct{}{}
	} else if strings.HasPrefix(key, "document:") {
		wanted["doc:"+strings.TrimPrefix(key, "document:")] = struct{}{}
	}
	var matches []map[string]any
	seen := map[string]bool{}
	for _, raw := range docs {
		doc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		identity := firstNonEmpty(anyString(doc["id"]), anyString(doc["ref"]), anyString(doc["handle"]))
		if seen[identity] {
			continue
		}
		for _, field := range []string{"id", "handle", "ref", "slug"} {
			if _, ok := wanted[anyString(doc[field])]; ok && anyString(doc[field]) != "" {
				matches = append(matches, doc)
				seen[identity] = true
				break
			}
		}
	}
	return matches
}

func findAutomaticReportDocuments(docs []any, title string) []map[string]any {
	wanted := reportMatchKey(title)
	var matches []map[string]any
	for _, raw := range docs {
		doc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if (strings.TrimSpace(anyString(doc["title"])) != "" && reportMatchKey(anyString(doc["title"])) == wanted) ||
			(strings.TrimSpace(anyString(doc["slug"])) != "" && reportMatchKey(anyString(doc["slug"])) == wanted) {
			matches = append(matches, doc)
		}
	}
	return matches
}

func reportMatchKey(value string) string {
	if key := reportSlug(value); key != "" {
		return key
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func reportCreateRequestKey(topicID, title string) string {
	keyTitle := reportSlug(title)
	if keyTitle == "" {
		keyTitle = strings.ToLower(strings.TrimSpace(title))
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(topicID) + "\x00" + keyTitle))
	return "visual-report-" + hex.EncodeToString(digest[:])
}

func reportResponseRevisionID(body map[string]any) string {
	return firstNonEmpty(anyString(extractNestedMap(body, "revision")["revision_id"]), anyString(body["revision_id"]))
}

func (a *App) readReportTarget(ctx context.Context, cfg config.Resolved, listed map[string]any) (map[string]any, *commandResult, error) {
	id := reportDocumentID(listed)
	if id == "" {
		return nil, nil, errnorm.Usage("invalid_response", "topic document list returned a document without an id")
	}
	result, err := a.getReportDocument(ctx, cfg, id)
	if err != nil {
		return nil, nil, err
	}
	doc := extractNestedMap(commandResultBody(result), "document")
	if len(doc) == 0 {
		doc = listed
	}
	return doc, result, nil
}

func isVisualReportDocument(read *commandResult) bool {
	if read == nil {
		return false
	}
	content, err := reportBytes(reportBodyContent(commandResultBody(read)))
	if err != nil {
		return false
	}
	return visualreport.Validate(content).Recognized
}

var nonSlug = regexp.MustCompile(`[^\pL\pN]+`)

func reportSlug(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"document:", "doc:"} {
		value = strings.TrimPrefix(value, prefix)
	}
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Trim(nonSlug.ReplaceAllString(value, "-"), "-")
}
func reportDocumentID(doc map[string]any) string {
	return firstNonEmpty(anyString(doc["id"]), anyString(doc["handle"]), anyString(doc["ref"]))
}
func reportDocumentRouteID(doc map[string]any, fallback string) string {
	return firstNonEmpty(anyString(doc["handle"]), anyString(doc["ref"]), anyString(doc["id"]), fallback)
}
func reportBytes(value any) ([]byte, error) {
	switch x := value.(type) {
	case string:
		return []byte(x), nil
	default:
		return json.Marshal(value)
	}
}
func (a *App) getReportDocument(ctx context.Context, cfg config.Resolved, ref string) (*commandResult, error) {
	return a.invokeTypedJSONWithIDResolution(ctx, cfg, "report publish", "docs.get", "document_id", ref, documentIDLookupSpec, nil, nil)
}

func (a *App) getReportRevision(ctx context.Context, cfg config.Resolved, documentID, revisionID string) (*commandResult, error) {
	return a.invokeDocsRevisionGetWithIDResolution(ctx, cfg, documentID, revisionID)
}
